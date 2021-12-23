package github

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jaberchez/custom-tekton-listener/pkg/config"
	"github.com/jaberchez/custom-tekton-listener/pkg/tekton"
	"github.com/jaberchez/custom-tekton-listener/pkg/utils"
)

type GitHub struct {
	ID          string
	HttpRequest *http.Request
	CheckIps    bool
	Payload     []byte
	GithubEvent string
}

func (g *GitHub) HandleRequest() {
	// Get query parameters
	queryParams := g.HttpRequest.URL.Query()

	if len(queryParams) == 0 {
		utils.Log("ERROR", "found empty parameters in http query request")
		return
	}

	var pipelineName, prefix string

	// Check if pipeline param exists in query string
	//
	// Note: "pipeline" and "prefix" are mandatory parameters
	for i, item := range []string{"pipeline", "prefix"} {
		paramFound, paramValue := getQueryParam(item, queryParams)

		if !paramFound {
			utils.Log("ERROR", fmt.Sprintf("%s param not found in query request", strings.ToUpper(item)))
			return
		}

		if len(paramValue) == 0 {
			utils.Log("ERROR", fmt.Sprintf("found empty value in http query param %s", strings.ToUpper(item)))
			return
		}

		if i == 0 {
			pipelineName = strings.ToLower(paramValue)
		} else {
			// Remove the last - or _ (if exists)
			if paramValue[len(paramValue)-1:] == "-" || paramValue[len(paramValue)-1:] == "_" {
				paramValue = paramValue[0 : len(paramValue)-1]
			}

			prefix = strings.ToLower(paramValue)
		}
	}

	// Get the configuration for this particuar pipeline
	pipelineConfig := config.GetPipeline(pipelineName)

	if pipelineConfig == nil {
		utils.Log("ERROR", fmt.Sprintf("pipeline %s not found in configuration", pipelineName))
		return
	}

	// Check if this webhook is a secure webhook
	//
	// Get password por this type of pipeline
	githubPass := pipelineConfig.GithubPassword

	if len(githubPass) == 0 {
		// Password for this particular pipeline not found, try global password
		githubPass = config.GetGlobalGithubPassword()
	}

	if len(githubPass) > 0 {
		// Is a secure webhook, check the signature
		ok, err := g.isValidSignature(githubPass)

		if err != nil {
			utils.Log("ERROR", err.Error())
			return
		}

		if !ok {
			utils.Log("ERROR", "wrong webhook signature")
			return
		}
	}

	// Check if we should run a pipeline
	pass, err := config.CheckWhenConditions(pipelineConfig.When, queryParams, g.HttpRequest, g.Payload)

	if err != nil {
		utils.Log("ERROR", err.Error())
		return
	}

	if !pass {
		utils.Log("INFO", "pipelinerun is not launched because does not meet the when conditions")
		return
	}

	// Create PipelineRun
	pipelineRun := &tekton.PipelineRun{
		ID:            g.ID,
		PipelineName:  pipelineName,
		Prefix:        prefix,
		GitHubPayload: g.Payload,
		GitHubEvent:   g.GithubEvent,
		Workspaces:    pipelineConfig.Workspaces,
		Resources:     pipelineConfig.Resources,
	}

	// Service account
	// Note: If service account is configured en global and particular pipeline, the service account
	//       of pipeline takes precedence
	serviceAccount := config.GetGlobalServiceAccount()

	if len(pipelineConfig.ServiceAccount) > 0 {
		serviceAccount = pipelineConfig.ServiceAccount
	}

	if len(serviceAccount) > 0 {
		pipelineRun.ServiceAccount = serviceAccount
	}

	// Set extra params
	//
	// Notes: - Extra params come from the query string and global and particular params from configmap
	//        - The order is global custom data, particular custom data and query http params
	extraParams := config.GetGlobalExtraParams()

	// Note: If a key exists in global extra params is overwriten
	for _, item := range pipelineConfig.ExtraParams {
		extraParams[item.Name] = item.Value
	}

	for k, v := range queryParams {
		// Store the parameters as they have been set
		extraParams[k] = v[0]
	}

	pipelineRun.ExtraParams = extraParams

	err = pipelineRun.Start()

	if err != nil {
		utils.Log("ERROR", err.Error())
	}

	utils.Log("INFO", "ok launched pipelinerun")
}

func getQueryParam(name string, params url.Values) (bool, string) {
	var paramValues []string
	var paramFound bool
	var paramValue string

	for k, val := range params {
		keyTmp := strings.ToLower(k)

		if keyTmp == name {
			paramFound = true
			paramValues = val
			break
		}
	}

	if len(paramValues) > 0 {
		// Return the first value
		paramValue = paramValues[0]
	}

	return paramFound, paramValue
}

func (g *GitHub) isValidSignature(secret string) (bool, error) {
	// Get X-Hub-Signature header
	signatureHeader := g.HttpRequest.Header.Get("X-Hub-Signature")

	if len(signatureHeader) == 0 {
		return false, errors.New("X-Hub-Signature header not found")
	}

	gotHash := strings.SplitN(signatureHeader, "=", 2)

	if gotHash[0] != "sha1" {
		return false, errors.New("sha1 not found")
	}

	hash := hmac.New(sha1.New, []byte(secret))

	if _, err := hash.Write(g.Payload); err != nil {
		return false, fmt.Errorf("cannot compute the HMAC for request: %s", err)
	}

	expectedHash := hex.EncodeToString(hash.Sum(nil))

	return gotHash[1] == expectedHash, nil
}
