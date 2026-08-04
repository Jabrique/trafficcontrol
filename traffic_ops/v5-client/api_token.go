package client

/*

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

import (
	"fmt"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_ops/toclientlib"
)

// apiUserAPITokens is the API version-relative path for the /user/api_tokens endpoint.
const apiUserAPITokens = "/user/api_tokens"

// CreateAPIToken creates a new API token for the authenticated user.
// The returned APITokenCreateResponse contains the plaintext token — it is shown once
// and cannot be retrieved again. Callers must store it immediately.
func (to *Session) CreateAPIToken(req tc.APITokenCreateRequest, opts RequestOptions) (tc.APITokenCreateResponse, toclientlib.ReqInf, error) {
	var response tc.APITokenCreateResponse
	reqInf, err := to.post(apiUserAPITokens, opts, req, &response)
	return response, reqInf, err
}

// GetAPITokens returns all non-expired API tokens visible to the authenticated user.
// Regular users see only their own tokens; admins see all tokens.
func (to *Session) GetAPITokens(opts RequestOptions) (tc.APITokensResponse, toclientlib.ReqInf, error) {
	var response tc.APITokensResponse
	reqInf, err := to.get(apiUserAPITokens, opts, &response)
	return response, reqInf, err
}

// GetAPIToken returns a single API token by its numeric ID.
func (to *Session) GetAPIToken(id int64, opts RequestOptions) (tc.APITokenSingleResponse, toclientlib.ReqInf, error) {
	var response tc.APITokenSingleResponse
	reqInf, err := to.get(fmt.Sprintf("%s/%d", apiUserAPITokens, id), opts, &response)
	return response, reqInf, err
}

// DeleteAPIToken deletes an API token by its numeric ID.
// Regular users can only delete their own tokens; admins can delete any token.
func (to *Session) DeleteAPIToken(id int64, opts RequestOptions) (tc.Alerts, toclientlib.ReqInf, error) {
	var response tc.Alerts
	reqInf, err := to.del(fmt.Sprintf("%s/%d", apiUserAPITokens, id), opts, &response)
	return response, reqInf, err
}
