package main

/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import (
	"fmt"
	"os"

	"github.com/apache/trafficcontrol/v8/cache-config/t3c-vector/config"
	"github.com/apache/trafficcontrol/v8/cache-config/t3c-vector/generate"
	"github.com/apache/trafficcontrol/v8/lib/go-log"
)

// AppName is used in log output and the version string.
const AppName = "t3c-vector"

// Version and GitRevision are injected at build time via -ldflags.
// Example: go build -ldflags "-X main.Version=8.0.0 -X main.GitRevision=abc1234"
var Version = "0.1"
var GitRevision = "nogit"

func main() {
	cfg, err := config.GetCfg(AppName, Version, GitRevision)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: getting config: %s\n", err.Error())
		os.Exit(1)
	}

	if err := log.InitCfg(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: initializing logger: %s\n", err.Error())
		os.Exit(1)
	}

	log.Infof("Starting %s/%s (%s)\n", AppName, Version, GitRevision)

	result, err := generate.Run(cfg)
	if err != nil {
		log.Errorf("vector config generation failed: %s\n", err.Error())
		os.Exit(2)
	}

	log.Infof("t3c-vector complete: %d written, %d removed, %d unchanged\n",
		result.Written, result.Removed, result.Unchanged)
}
