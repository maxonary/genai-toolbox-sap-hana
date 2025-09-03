// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v20250326

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/googleapis/genai-toolbox/internal/auth"
	"github.com/googleapis/genai-toolbox/internal/server/mcp/jsonrpc"
	"github.com/googleapis/genai-toolbox/internal/tools"
	"github.com/googleapis/genai-toolbox/internal/util"
)

// ProcessMethod returns a response for the request.
func ProcessMethod(ctx context.Context, id jsonrpc.RequestId, method string, toolset tools.Toolset, tools map[string]tools.Tool, authServices map[string]auth.AuthService, body []byte, header http.Header) (any, error) {
	switch method {
	case PING:
		return pingHandler(id)
	case TOOLS_LIST:
		return toolsListHandler(id, toolset, body)
	case TOOLS_CALL:
		return toolsCallHandler(ctx, id, tools, authServices, body, header)
	default:
		err := fmt.Errorf("invalid method %s", method)
		return jsonrpc.NewError(id, jsonrpc.METHOD_NOT_FOUND, err.Error(), nil), err
	}
}

// pingHandler handles the "ping" method by returning an empty response.
func pingHandler(id jsonrpc.RequestId) (any, error) {
	return jsonrpc.JSONRPCResponse{
		Jsonrpc: jsonrpc.JSONRPC_VERSION,
		Id:      id,
		Result:  struct{}{},
	}, nil
}

func toolsListHandler(id jsonrpc.RequestId, toolset tools.Toolset, body []byte) (any, error) {
	var req ListToolsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		err = fmt.Errorf("invalid mcp tools list request: %w", err)
		return jsonrpc.NewError(id, jsonrpc.INVALID_REQUEST, err.Error(), nil), err
	}

	result := ListToolsResult{
		Tools: toolset.McpManifest,
	}
	return jsonrpc.JSONRPCResponse{
		Jsonrpc: jsonrpc.JSONRPC_VERSION,
		Id:      id,
		Result:  result,
	}, nil
}

// toolsCallHandler generate a response for tools call.
func toolsCallHandler(ctx context.Context, id jsonrpc.RequestId, toolsMap map[string]tools.Tool, authServices map[string]auth.AuthService, body []byte, header http.Header) (any, error) {
	// retrieve logger from context
	logger, err := util.LoggerFromContext(ctx)
	if err != nil {
		return jsonrpc.NewError(id, jsonrpc.INTERNAL_ERROR, err.Error(), nil), err
	}

	var req CallToolRequest
	if err = json.Unmarshal(body, &req); err != nil {
		err = fmt.Errorf("invalid mcp tools call request: %w", err)
		return jsonrpc.NewError(id, jsonrpc.INVALID_REQUEST, err.Error(), nil), err
	}

	toolName := req.Params.Name
	toolArgument := req.Params.Arguments
	logger.DebugContext(ctx, fmt.Sprintf("tool name: %s", toolName))
	tool, ok := toolsMap[toolName]
	if !ok {
		err = fmt.Errorf("invalid tool name: tool with name %q does not exist", toolName)
		return jsonrpc.NewError(id, jsonrpc.INVALID_PARAMS, err.Error(), nil), err
	}

	// Get access token
	accessToken := tools.AccessToken(header.Get("Authorization"))

	// Check if this specific tool requires the standard authorization header
	if tool.RequiresClientAuthorization() {
		if accessToken == "" {
			return jsonrpc.NewError(id, jsonrpc.INVALID_REQUEST, "missing access token in the 'Authorization' header", nil), tools.ErrUnauthorized
		}
	}

	// marshal arguments and decode it using decodeJSON instead to prevent loss between floats/int.
	aMarshal, err := json.Marshal(toolArgument)
	if err != nil {
		err = fmt.Errorf("unable to marshal tools argument: %w", err)
		return jsonrpc.NewError(id, jsonrpc.INTERNAL_ERROR, err.Error(), nil), err
	}

	var data map[string]any
	if err = util.DecodeJSON(bytes.NewBuffer(aMarshal), &data); err != nil {
		err = fmt.Errorf("unable to decode tools argument: %w", err)
		return jsonrpc.NewError(id, jsonrpc.INTERNAL_ERROR, err.Error(), nil), err
	}

	// Tool authentication
	// claimsFromAuth maps the name of the authservice to the claims retrieved from it.
	claimsFromAuth := make(map[string]map[string]any)

	// if using stdio, header will be nil and auth will not be supported
	if header != nil {
		for _, aS := range authServices {
			claims, err := aS.GetClaimsFromHeader(ctx, header)
			if err != nil {
				logger.DebugContext(ctx, err.Error())
				continue
			}
			if claims == nil {
				// authService not present in header
				continue
			}
			claimsFromAuth[aS.GetName()] = claims
		}
	}

	// Tool authorization check
	verifiedAuthServices := make([]string, len(claimsFromAuth))
	i := 0
	for k := range claimsFromAuth {
		verifiedAuthServices[i] = k
		i++
	}

	// Check if any of the specified auth services is verified
	isAuthorized := tool.Authorized(verifiedAuthServices)
	if !isAuthorized {
		err = fmt.Errorf("unauthorized Tool call: Please make sure your specify correct auth headers: %w", tools.ErrUnauthorized)
		return jsonrpc.NewError(id, jsonrpc.INVALID_REQUEST, err.Error(), nil), err
	}
	logger.DebugContext(ctx, "tool invocation authorized")

	params, err := tool.ParseParams(data, claimsFromAuth)
	if err != nil {
		err = fmt.Errorf("provided parameters were invalid: %w", err)
		return jsonrpc.NewError(id, jsonrpc.INVALID_PARAMS, err.Error(), nil), err
	}
	logger.DebugContext(ctx, fmt.Sprintf("invocation params: %s", params))

	// run tool invocation and generate response.
	results, err := tool.Invoke(ctx, params, accessToken)
	if err != nil {
		errStr := err.Error()
		// Missing authService tokens.
		if errors.Is(err, tools.ErrUnauthorized) {
			return jsonrpc.NewError(id, jsonrpc.INVALID_REQUEST, err.Error(), nil), err
		}
		// Upstream auth error
		if strings.Contains(errStr, "Error 401") || strings.Contains(errStr, "Error 403") {
			if tool.RequiresClientAuthorization() {
				// Error with client credentials should pass down to the client
				return jsonrpc.NewError(id, jsonrpc.INVALID_REQUEST, err.Error(), nil), err
			}
			// Auth error with ADC should raise internal 500 error
			return jsonrpc.NewError(id, jsonrpc.INTERNAL_ERROR, err.Error(), nil), err
		}
		text := TextContent{
			Type: "text",
			Text: err.Error(),
		}
		return jsonrpc.JSONRPCResponse{
			Jsonrpc: jsonrpc.JSONRPC_VERSION,
			Id:      id,
			Result:  CallToolResult{Content: []TextContent{text}, IsError: true},
		}, nil
	}

	content := make([]TextContent, 0)

	sliceRes, ok := results.([]any)
	if !ok {
		sliceRes = []any{results}
	}

	for _, d := range sliceRes {
		text := TextContent{Type: "text"}
		dM, err := json.Marshal(d)
		if err != nil {
			text.Text = fmt.Sprintf("fail to marshal: %s, result: %s", err, d)
		} else {
			text.Text = string(dM)
		}
		content = append(content, text)
	}

	return jsonrpc.JSONRPCResponse{
		Jsonrpc: jsonrpc.JSONRPC_VERSION,
		Id:      id,
		Result:  CallToolResult{Content: content},
	}, nil
}
