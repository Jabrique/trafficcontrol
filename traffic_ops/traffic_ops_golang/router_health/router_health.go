package router_health

/*
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/config"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/crconfig"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/monitoring"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/util/monitorhlp"

	"github.com/jmoiron/sqlx"
)

const (
	autoOfflinePrefix    = "[auto-tm]:"
	autoOfflineReason    = "[auto-tm]: unreachable per TM quorum"
	defaultPollInterval  = 5 * time.Second
	defaultDownThreshold = 1
	defaultUpThreshold   = 2
)

// Watcher polls Traffic Monitor CRStates on a fixed interval and automatically
// sets unreachable Traffic Routers to OFFLINE. When TM quorum confirms a
// previously auto-OFFLINE router has recovered, it restores it to REPORTED.
type Watcher struct {
	db            *sqlx.DB
	toHost        string
	toVersion     string
	downCounts    map[string]int  // hostname -> consecutive TM "down" count
	autoOffline   map[string]bool // hostname -> did THIS watcher set it OFFLINE?
	upCounts      map[string]int  // hostname -> consecutive TM "up" count
	mu            sync.Mutex
	pollInterval  time.Duration
	downThreshold int
	upThreshold   int
}

// NewWatcher creates a Watcher and pre-populates the autoOffline map from any
// Traffic Routers already OFFLINE with our prefix tag in the database, so
// recovery detection survives Traffic Ops process restarts.
func NewWatcher(db *sqlx.DB, cfg config.Config) *Watcher {
	w := &Watcher{
		db:            db,
		toHost:        cfg.URL.Host,
		toVersion:     cfg.Version,
		downCounts:    make(map[string]int),
		autoOffline:   make(map[string]bool),
		upCounts:      make(map[string]int),
		pollInterval:  defaultPollInterval,
		downThreshold: defaultDownThreshold,
		upThreshold:   defaultUpThreshold,
	}
	w.repopulateAutoOffline()
	return w
}

// Start runs the watch loop, polling every pollInterval until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Watcher) poll(ctx context.Context) {
	tx, err := w.db.DB.BeginTx(ctx, nil)
	if err != nil {
		log.Errorf("RouterHealthWatcher: begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	client, err := monitorhlp.GetClient(tx)
	if err != nil {
		log.Errorf("RouterHealthWatcher: get monitor HTTP client: %v", err)
		return
	}

	monitorURLs, err := monitorhlp.GetURLs(tx)
	if err != nil {
		log.Errorf("RouterHealthWatcher: get monitor URLs: %v", err)
		return
	}

	for cdnName, tmFQDNs := range monitorURLs {
		var routerStates map[tc.RouterName]tc.IsAvailable
		for _, tmFQDN := range tmFQDNs {
			crStates, err := monitorhlp.GetCRStates(tmFQDN, client)
			if err != nil {
				log.Warnf("RouterHealthWatcher: get CRStates from %s: %v", tmFQDN, err)
				continue
			}
			routerStates = crStates.Routers
			break // first reachable TM gives us the quorum result
		}
		if routerStates == nil {
			log.Debugf("RouterHealthWatcher: no reachable TM for CDN %s, skipping", cdnName)
			continue
		}

		for routerName, state := range routerStates {
			w.processRouterState(tx, string(cdnName), string(routerName), state.IsAvailable)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("RouterHealthWatcher: commit transaction: %v", err)
	}
}

// processRouterState handles both down detection and recovery for a single router
// in a single TM poll cycle.
func (w *Watcher) processRouterState(tx *sql.Tx, cdnName, hostname string, isAvailable bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !isAvailable {
		w.upCounts[hostname] = 0
		w.downCounts[hostname]++

		if w.downCounts[hostname] < w.downThreshold {
			return
		}
		if w.autoOffline[hostname] {
			return // already managed by us
		}

		serverID, currentStatus, err := w.getRouterStatus(tx, hostname)
		if err != nil {
			log.Debugf("RouterHealthWatcher: getRouterStatus(%s): %v", hostname, err)
			return
		}
		if currentStatus != string(tc.CacheStatusReported) {
			return // only auto-manage routers that are currently REPORTED
		}

		reason := autoOfflineReason
		if err := w.setStatus(tx, serverID, "OFFLINE", &reason); err != nil {
			log.Errorf("RouterHealthWatcher: set OFFLINE for %s: %v", hostname, err)
			return
		}
		if err := w.snapshot(tx, cdnName); err != nil {
			log.Warnf("RouterHealthWatcher: snapshot after OFFLINE for %s: %v", hostname, err)
			// non-fatal: TR catches up on its next CRConfig poll
		}

		w.autoOffline[hostname] = true
		w.downCounts[hostname] = 0
		log.Infof("RouterHealthWatcher: auto-set OFFLINE for TR %s (CDN: %s)", hostname, cdnName)

	} else {
		w.downCounts[hostname] = 0

		if !w.autoOffline[hostname] {
			return // not managed by us
		}

		w.upCounts[hostname]++
		if w.upCounts[hostname] < w.upThreshold {
			return
		}

		serverID, currentStatus, err := w.getRouterStatus(tx, hostname)
		if err != nil {
			log.Debugf("RouterHealthWatcher: getRouterStatus(%s): %v", hostname, err)
			return
		}
		// Operator may have changed status manually -- stop tracking.
		if currentStatus != string(tc.CacheStatusOffline) {
			w.autoOffline[hostname] = false
			w.upCounts[hostname] = 0
			return
		}
		// Verify offline_reason tag is still ours before restoring.
		if !w.hasAutoTag(tx, serverID) {
			w.autoOffline[hostname] = false
			w.upCounts[hostname] = 0
			return
		}

		if err := w.setStatus(tx, serverID, string(tc.CacheStatusReported), nil); err != nil {
			log.Errorf("RouterHealthWatcher: restore REPORTED for %s: %v", hostname, err)
			return
		}
		if err := w.snapshot(tx, cdnName); err != nil {
			log.Warnf("RouterHealthWatcher: snapshot after restore for %s: %v", hostname, err)
		}

		w.autoOffline[hostname] = false
		w.upCounts[hostname] = 0
		log.Infof("RouterHealthWatcher: restored TR %s to REPORTED (CDN: %s)", hostname, cdnName)
	}
}

// getRouterStatus returns the database ID and current status name for a CCR-type server.
func (w *Watcher) getRouterStatus(tx *sql.Tx, hostname string) (int, string, error) {
	const q = `
SELECT s.id, st.name
FROM   server s
JOIN   type t    ON t.id  = s.type
JOIN   status st ON st.id = s.status
WHERE  s.host_name = $1
AND    t.name      = 'CCR'
`
	var serverID int
	var statusName string
	if err := tx.QueryRow(q, hostname).Scan(&serverID, &statusName); err != nil {
		return 0, "", err
	}
	return serverID, statusName, nil
}

// hasAutoTag returns true when the server offline_reason starts with our prefix.
func (w *Watcher) hasAutoTag(tx *sql.Tx, serverID int) bool {
	var reason sql.NullString
	if err := tx.QueryRow(`SELECT offline_reason FROM server WHERE id = $1`, serverID).Scan(&reason); err != nil {
		return false
	}
	return reason.Valid && strings.HasPrefix(reason.String, autoOfflinePrefix)
}

// setStatus updates status and offline_reason for a server, mirroring the SQL
// used by traffic_ops_golang/server/put_status.go.
func (w *Watcher) setStatus(tx *sql.Tx, serverID int, newStatus string, offlineReason *string) error {
	const q = `
UPDATE server
SET    status = (SELECT id FROM status WHERE name = $1),
       offline_reason = $2,
       status_last_updated = NOW()
WHERE  id = $3
`
	_, err := tx.Exec(q, newStatus, offlineReason, serverID)
	return err
}

// snapshot triggers a CRConfig + monitoring snapshot for the named CDN.
func (w *Watcher) snapshot(tx *sql.Tx, cdnName string) error {
	crc, err := crconfig.Make(tx, cdnName, "system", w.toHost, w.toVersion, false, false)
	if err != nil {
		return err
	}
	monJSON, err := monitoring.GetMonitoringJSON(tx, cdnName)
	if err != nil {
		return err
	}
	return crconfig.Snapshot(tx, crc, monJSON)
}

// repopulateAutoOffline queries the database on startup for any Traffic Router
// already OFFLINE with our tag, so recovery detection survives TO restarts.
func (w *Watcher) repopulateAutoOffline() {
	tx, err := w.db.DB.Begin()
	if err != nil {
		log.Errorf("RouterHealthWatcher: repopulate begin tx: %v", err)
		return
	}
	defer tx.Rollback()

	const q = `
SELECT s.host_name
FROM   server s
JOIN   type t    ON t.id  = s.type
JOIN   status st ON st.id = s.status
WHERE  t.name  = 'CCR'
AND    st.name = 'OFFLINE'
AND    s.offline_reason LIKE '[auto-tm]:%'
`
	rows, err := tx.Query(q)
	if err != nil {
		log.Errorf("RouterHealthWatcher: repopulate query: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var hostname string
		if err := rows.Scan(&hostname); err != nil {
			log.Errorf("RouterHealthWatcher: repopulate scan: %v", err)
			continue
		}
		w.autoOffline[hostname] = true
		count++
	}
	if err := rows.Err(); err != nil {
		log.Errorf("RouterHealthWatcher: repopulate rows err: %v", err)
	}
	if count > 0 {
		log.Infof("RouterHealthWatcher: repopulated %d auto-OFFLINE TR(s) from DB on startup", count)
	}
	tx.Commit()
}
