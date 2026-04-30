package org.apache.traffic_control.traffic_router.core.ds;

/*
 *
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

import org.apache.traffic_control.traffic_router.core.config.ConfigHandler;
import org.apache.traffic_control.traffic_router.core.util.AbstractResourceWatcher;
import org.apache.traffic_control.traffic_router.core.util.JsonUtils;
import org.apache.traffic_control.traffic_router.core.util.JsonUtilsException;
import org.apache.traffic_control.traffic_router.core.util.TrafficOpsUtils;
import com.fasterxml.jackson.core.JsonFactory;
import com.fasterxml.jackson.core.JsonParseException;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import org.apache.logging.log4j.LogManager;
import org.apache.logging.log4j.Logger;

import java.io.*;
import java.util.HashMap;
import java.util.Iterator;
import java.util.List;

public class LetsEncryptDnsChallengeWatcher extends AbstractResourceWatcher {
    private static final Logger LOGGER = LogManager.getLogger(LetsEncryptDnsChallengeWatcher.class);
    public static final String DEFAULT_LE_DNS_CHALLENGE_URL = "https://${toHostname}/api/"+TrafficOpsUtils.TO_API_VERSION+"/letsencrypt/dnsrecords/";

    private String configFile;
    private ConfigHandler configHandler;

    public LetsEncryptDnsChallengeWatcher() {
        setDatabaseUrl(DEFAULT_LE_DNS_CHALLENGE_URL);
        setDefaultDatabaseUrl(DEFAULT_LE_DNS_CHALLENGE_URL);
    }

    @Override
    public boolean useData(final String data) {
        try {
            final ObjectMapper mapper = new ObjectMapper(new JsonFactory());
            final HashMap<String, List<LetsEncryptDnsChallenge>> dataMap = mapper.readValue(data, new TypeReference<HashMap<String, List<LetsEncryptDnsChallenge>>>() { });
            final List<LetsEncryptDnsChallenge> challengeList = dataMap.get("response");

            // Prefer the in-memory cached CRConfig over the disk file.
            // The disk file can be stale if a CRConfig snapshot was applied by TrafficMonitorWatcher
            // after a previous challenge injection. Reading from memory guarantees we always inject
            // into the most recently accepted config, not one that may have already been superseded.
            final String baseConfigJson = configHandler.getLastValidCrConfigJson();
            final JsonNode mostRecentConfig;
            if (baseConfigJson != null) {
                LOGGER.info("LetsEncryptDnsChallengeWatcher: using in-memory CRConfig as injection base");
                mostRecentConfig = mapper.readTree(baseConfigJson);
            } else {
                // Fallback for startup race: no config has been processed yet, read from disk.
                LOGGER.info("LetsEncryptDnsChallengeWatcher: no in-memory CRConfig available, falling back to disk file");
                final String diskConfig = readConfigFile();
                if (diskConfig == null) {
                    LOGGER.error("LetsEncryptDnsChallengeWatcher: cannot inject challenges, both in-memory and disk CRConfig are unavailable");
                    return false;
                }
                mostRecentConfig = mapper.readTree(diskConfig);
            }

            final ObjectNode deliveryServicesNode = (ObjectNode) JsonUtils.getJsonNode(mostRecentConfig, ConfigHandler.deliveryServicesKey);


            challengeList.forEach(challenge -> {
                final ObjectNode deliveryServiceConfig = (ObjectNode) deliveryServicesNode.get(challenge.getXmlId());
                if (deliveryServiceConfig == null) {
                    LOGGER.error("finding deliveryservice in cr-config for " + challenge.getXmlId());
                    return;
                }

                String staticEntryString = challenge.getFqdn();
                final ArrayNode domains = (ArrayNode) deliveryServiceConfig.get("domains");
                if (domains == null || domains.size() == 0) {
                    LOGGER.error("no domains found in cr-config for deliveryservice " + challenge.getXmlId());
                    return;
                }

                final Iterator<JsonNode> domainIter = domains.iterator();
                while(domainIter.hasNext()) {
                    final JsonNode domainNode = domainIter.next();
                    staticEntryString = staticEntryString.replace(domainNode.asText() + ".", "");
                }

                if (staticEntryString.endsWith(".")) {
                    staticEntryString = staticEntryString.substring(0, staticEntryString.length() - 1);
                }

                final ArrayNode staticDnsEntriesNode = updateStaticEntries(challenge, staticEntryString, mapper, deliveryServiceConfig);

                deliveryServiceConfig.set("staticDnsEntries", staticDnsEntriesNode);
                deliveryServicesNode.set(challenge.getXmlId(), deliveryServiceConfig);

            });

            final ObjectNode fullConfig = (ObjectNode) mostRecentConfig;
            fullConfig.set(ConfigHandler.deliveryServicesKey, deliveryServicesNode);

            // NOTE: We intentionally do NOT bump stats.date here.
            // Previously the code set stats.date = Instant.now() to bypass the snapshot timestamp
            // guard in ConfigHandler.processConfig(). This was incorrect: it caused the
            // lastSnapshotTimestamp to advance to the current wall clock, which would then
            // reject any legitimate CRConfig snapshot from Traffic Monitor that was generated
            // before that moment.
            //
            // Instead, we now call processConfigForDnsChallenge() which has an explicit bypass
            // for the timestamp guard and does NOT update lastSnapshotTimestamp, ensuring
            // real CRConfig updates from Traffic Monitor are never blocked.
            try {
                configHandler.processConfigForDnsChallenge(fullConfig.toString());
            } catch (JsonParseException | JsonUtilsException jsonError) {
                LOGGER.error("error processing config: " + jsonError.getMessage());
            }

            return true;
        } catch (Exception e) {
            LOGGER.warn("Failed updating dns challenge txt record with data from " + dataBaseURL + ":", e);
        }

        return false;
    }

    @Override
    protected boolean verifyData(final String data) {
        try {
            final ObjectMapper mapper = new ObjectMapper(new JsonFactory());
            mapper.readValue(data, new TypeReference<HashMap<String, List<LetsEncryptDnsChallenge>>>() { });
            return true;
        } catch (Exception e) {
            LOGGER.warn("Failed to build dns challenge data while verifying:", e);
        }

        return false;
    }

    @Override
    public String getWatcherConfigPrefix() {
        return "dnschallengemapping";
    }

    private String readConfigFile() {
        try (InputStream is = new FileInputStream(databasesDirectory.resolve(configFile).toString());
             BufferedReader buf = new BufferedReader(new InputStreamReader(is))
        ) {
            String line = buf.readLine();
            final StringBuilder sb = new StringBuilder();
            while (line != null) {
                sb.append(line).append('\n');
                line = buf.readLine();
            }
            return sb.toString();
        } catch (Exception e) {
            LOGGER.error("Could not read cr-config file " + configFile + ":", e);
            return null;
        }
    }

    private ArrayNode updateStaticEntries(final LetsEncryptDnsChallenge challenge, final String name, final ObjectMapper mapper, final ObjectNode deliveryServiceConfig) {
        ArrayNode staticDnsEntriesNode = mapper.createArrayNode();
        ArrayNode newStaticDnsEntriesNode = mapper.createArrayNode();

        if (deliveryServiceConfig.findValue("staticDnsEntries") != null) {
            staticDnsEntriesNode = (ArrayNode) deliveryServiceConfig.findValue("staticDnsEntries");
        }

        if (challenge.getRecord().isEmpty()) {
            for (int i = 0; i < staticDnsEntriesNode.size(); i++) {
                if (!staticDnsEntriesNode.get(i).get("name").equals(name)) {
                    newStaticDnsEntriesNode.add(i);
                }
            }
        } else {
            newStaticDnsEntriesNode = staticDnsEntriesNode;

            final ObjectNode newChildNode = mapper.createObjectNode();
            newChildNode.put("type", "TXT");
            newChildNode.put("name", name);
            newChildNode.put("value", challenge.getRecord());
            newChildNode.put("ttl", 10);

            newStaticDnsEntriesNode.add(newChildNode);
        }

        return newStaticDnsEntriesNode;
    }

    /**
     * Injects active Let's Encrypt DNS challenges from the watcher's locally cached
     * challenge file directly into the provided CRConfig JSON tree.
     *
     * <p>This method is called by {@link org.apache.traffic_control.traffic_router.core.config.ConfigHandler}
     * on every real CRConfig snapshot reload. It ensures that active challenges survive
     * a CRConfig reload even when {@link #useData} is not called because Traffic Ops
     * returned HTTP 304 (challenge data unchanged). Without this, a challenge injected
     * at T=0 would be silently wiped by the next CRConfig snapshot and never
     * re-injected until the challenge data itself changes.
     *
     * <p>Reads {@code databasesDirectory/databaseName} — the watcher's own cached copy
     * of the {@code /letsencrypt/dnsrecords/} response — which is kept up to date
     * independently of the CRConfig lifecycle.
     *
     * @param mapper     the ObjectMapper to use for JSON parsing
     * @param configRoot the mutable root ObjectNode of the CRConfig being processed
     */
    public void injectActiveChallengesInto(final ObjectMapper mapper, final ObjectNode configRoot) {
        if (databaseName == null || databasesDirectory == null) {
            return;
        }
        final File dbFile = databasesDirectory.resolve(databaseName).toFile();
        if (!dbFile.exists() || dbFile.length() == 0) {
            return;
        }

        try {
            final StringBuilder sb = new StringBuilder();
            try (BufferedReader br = new BufferedReader(new FileReader(dbFile))) {
                String line;
                while ((line = br.readLine()) != null) {
                    sb.append(line);
                }
            }

            final HashMap<String, List<LetsEncryptDnsChallenge>> dataMap =
                mapper.readValue(sb.toString(), new TypeReference<HashMap<String, List<LetsEncryptDnsChallenge>>>() { });
            final List<LetsEncryptDnsChallenge> challengeList = dataMap.get("response");
            if (challengeList == null || challengeList.isEmpty()) {
                return;
            }

            final ObjectNode deliveryServicesNode =
                (ObjectNode) JsonUtils.getJsonNode(configRoot, ConfigHandler.deliveryServicesKey);

            challengeList.forEach(challenge -> {
                final ObjectNode deliveryServiceConfig =
                    (ObjectNode) deliveryServicesNode.get(challenge.getXmlId());
                if (deliveryServiceConfig == null) {
                    LOGGER.warn("injectActiveChallengesInto: DS '" + challenge.getXmlId() + "' not found in CRConfig; skipping");
                    return;
                }

                String staticEntryString = challenge.getFqdn();
                final ArrayNode domains = (ArrayNode) deliveryServiceConfig.get("domains");
                if (domains == null || domains.size() == 0) {
                    LOGGER.warn("injectActiveChallengesInto: no domains for DS '" + challenge.getXmlId() + "'; skipping");
                    return;
                }

                final Iterator<JsonNode> domainIter = domains.iterator();
                while (domainIter.hasNext()) {
                    staticEntryString = staticEntryString.replace(domainIter.next().asText() + ".", "");
                }
                if (staticEntryString.endsWith(".")) {
                    staticEntryString = staticEntryString.substring(0, staticEntryString.length() - 1);
                }

                final ArrayNode updatedEntries =
                    updateStaticEntries(challenge, staticEntryString, mapper, deliveryServiceConfig);
                deliveryServiceConfig.set("staticDnsEntries", updatedEntries);
                deliveryServicesNode.set(challenge.getXmlId(), deliveryServiceConfig);
            });

            configRoot.set(ConfigHandler.deliveryServicesKey, deliveryServicesNode);
            LOGGER.info("injectActiveChallengesInto: injected " + challengeList.size()
                + " active DNS challenge(s) into CRConfig snapshot");
        } catch (Exception e) {
            LOGGER.warn("injectActiveChallengesInto: failed to inject active challenges into CRConfig", e);
        }
    }

    public void setConfigHandler(final ConfigHandler configHandler) {
        this.configHandler = configHandler;
    }
    public ConfigHandler getConfigHandler() {
        return this.configHandler;
    }

    public void setConfigFile(final String configFile) {
        this.configFile = configFile;
    }
}
