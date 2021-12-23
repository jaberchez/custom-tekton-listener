package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jaberchez/custom-tekton-listener/pkg/k8s"
	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v2"
)

const (
	VolumeClaimTemplateType   string = "volumeClaimTemplate"
	PersistentVolumeClaimType string = "persistentVolumeClaim"
	EmptyDirType              string = "emptyDir"
	ConfigmapType             string = "configmap"
	SecretType                string = "secret"

	whenKindHeader  string = "header"
	whenKindPayload string = "payload"
	whenKindQuery   string = "query"

	valueOperatorEqual       string = "equal"
	valueOperatorNotEqual    string = "notequal"
	valueOperatorContains    string = "contains"
	valueOperatorNotContains string = "notcontains"
)

var (
	configuration config

	workspacesTypes []string = []string{VolumeClaimTemplateType, PersistentVolumeClaimType,
		EmptyDirType, ConfigmapType, SecretType}

	whenKinds []string = []string{whenKindHeader, whenKindPayload, whenKindQuery}

	valuesOperators []string = []string{valueOperatorEqual, valueOperatorNotEqual,
		valueOperatorContains, valueOperatorNotContains}
)

// See the file configmap-config.yaml to check the configuration
type config struct {
	GlobalGitHubSecretName string      `yaml:"globalGithubSecretName",ommitempty`
	GlobalExtraParams      []ParamItem `yaml:"globalExtraParams",ommitempty`
	GlobalServiceAccount   string      `yaml:"globalServiceAccount",ommitempty`
	GlobalGithubPassword   string
	Pipelines              []Pipeline `yaml:"pipelines"`
	Resources              []Resource `yaml:"resources"`
	GitHubIps              []string
}

type Pipeline struct {
	Name             string      `yaml:"name"`
	ExtraParams      []ParamItem `yaml:"extraParams",ommitempty`
	Workspaces       []Workspace `yaml:"workspaces",ommitempty`
	Resources        []Resource  `yaml:"resources",ommitempty`
	GithubSecretName string      `yaml:"githubSecretName",ommitempty`
	GithubPassword   string
	ServiceAccount   string     `yaml:"serviceAccount",ommitempty`
	When             []WhenItem `yaml:"when",ommitempty`
}

type ParamItem struct {
	Name  string `yaml:"name",ommitempty`
	Value string `yaml:"value",ommitempty`
}

type Workspace struct {
	Name string `yaml:"name",ommitempty`
	Type string `yaml:"type",ommitempty`
	Data map[string]string
}

type Resource struct {
	Name        string `yaml:"name",ommitempty`
	ResourceRef string `yaml:"resourceRef",ommitempty`
}

type WhenItem struct {
	Kind   string      `yaml:"kind",ommitempty`
	Keys   []string    `yaml:"keys",ommitempty`
	Values []ValueItem `yaml:"values",ommitempty`
}

type ValueItem struct {
	Operator string `yaml:"operator",ommitempty`
	Data     string `yaml:"data",ommitempty`
}

func LoadConfig(haveToLoadGethubIps bool) error {
	// Name of ConfigMap. The format is namethisapplication-config and
	// must be stored in the same Namespace where the pod is running
	//
	// Example: custom-tekton-listener-config
	nameConfigMap := fmt.Sprintf("%s-config", filepath.Base(os.Args[0]))

	configmap, err := k8s.GetConfigMap(nameConfigMap, os.Getenv("POD_NAMESPACE"))

	if err != nil {
		return err
	}

	config, ok := configmap.Data["config"]

	if !ok {
		return fmt.Errorf("key config not found in ConfigMap %s", nameConfigMap)
	}

	// Load ConfigMap
	err = yaml.Unmarshal([]byte(config), &configuration)

	if err != nil {
		return err
	}
	// Check if webhook has global password
	if len(configuration.GlobalGitHubSecretName) > 0 {
		// Get Secret
		secret, err := k8s.GetSecret(configuration.GlobalGitHubSecretName, os.Getenv("POD_NAMESPACE"))

		if err != nil {
			return err
		}

		pass, ok := secret.Data["password"]

		if !ok {
			return fmt.Errorf("field password not found in Secret %s", configuration.GlobalGitHubSecretName)
		}

		configuration.GlobalGithubPassword = string(pass)
	}

	// Check if pipelines have password
	for i, p := range configuration.Pipelines {
		if len(p.GithubSecretName) > 0 {
			// Get Secret
			secret, err := k8s.GetSecret(p.GithubSecretName, os.Getenv("POD_NAMESPACE"))

			if err != nil {
				return err
			}

			pass, ok := secret.Data["password"]

			if !ok {
				return fmt.Errorf("field password not found in Secret %s", configuration.GlobalGitHubSecretName)
			}

			configuration.Pipelines[i].GithubPassword = string(pass)
		}

		// Check Worspaces
		if len(p.Workspaces) > 0 {
			err := checkWorkspacesConfig(p.Workspaces)

			if err != nil {
				return err
			}

			// Get data from workspaces
			err = getkWorkspacesData(i, p.Workspaces)

			if err != nil {
				return err
			}
		}
	}

	if haveToLoadGethubIps {
		githubIps, err := loadGithubIps()

		if err != nil {
			return err
		}

		configuration.GitHubIps = githubIps
	}

	return nil
}

func ParseConfig() error {
	if len(configuration.Pipelines) == 0 {
		return errors.New("pipelines field is empty")
	}

	for _, p := range configuration.Pipelines {
		// Check name pipeline
		if len(p.Name) == 0 {
			return fmt.Errorf("pipeline name is empty")
		}

		pipelineName := strings.ToLower(p.Name)

		if !isValidPipeline(pipelineName) {
			return fmt.Errorf("pipeline name unknown: %s", pipelineName)
		}

		// Check extra params
		//
		// Note: We don't check the value because the value itself could be empty
		if len(p.ExtraParams) > 0 {
			for _, e := range p.ExtraParams {
				if len(e.Name) == 0 {
					return errors.New("found an empty name in extra params")
				}
			}
		}

		// Check Resources
		if len(p.Resources) > 0 {
			for _, r := range p.Resources {
				if len(r.Name) == 0 {
					return errors.New("found an empty name in resources")
				}

				if len(r.ResourceRef) == 0 {
					return errors.New("found an empty resourceRef in resources")
				}
			}
		}

		// Check when conditions
		if len(p.When) > 0 {
			err := parseWhen(p.When)

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func PipelineExists(pipelineName string) bool {
	var exists bool

	for _, item := range configuration.Pipelines {
		if item.Name == pipelineName {
			exists = true
			break
		}
	}

	return exists
}

func GetGlobalGithubPassword() string {
	return configuration.GlobalGithubPassword
}

func GetGlobalServiceAccount() string {
	return configuration.GlobalServiceAccount
}

func GetPipeline(pipelineName string) *Pipeline {
	for _, item := range configuration.Pipelines {
		if item.Name == strings.ToLower(pipelineName) {
			return &item
		}
	}
	return nil
}

func GetGlobalExtraParams() map[string]string {
	extraParams := make(map[string]string)

	for _, item := range configuration.GlobalExtraParams {
		extraParams[item.Name] = item.Value
	}

	return extraParams
}

func GetGithubIps() []string {
	return configuration.GitHubIps
}

func CheckWhenConditions(when []WhenItem, queryParams url.Values, r *http.Request, payload []byte) (bool, error) {
	var totalMatches int = 0

	for _, whenItem := range when {
		var match bool
		var err error

		switch strings.ToLower(whenItem.Kind) {
		case whenKindPayload:
			match, err = checkPayloadCondition(whenItem, payload)
		case whenKindHeader:
			match, err = checkHttpHeadersCondition(whenItem, r.Header)
		case whenKindQuery:
			match, err = checkQueryParamsCondition(whenItem, queryParams)
		}

		if err != nil {
			return false, err
		}

		if match {
			totalMatches++
		} else {
			// Match not found. It is not necessary to check more because they
			// have to be fulfilled all
			break
		}
	}

	return totalMatches == len(when), nil
}

func checkPayloadCondition(whenItem WhenItem, payload []byte) (bool, error) {
	for _, k := range whenItem.Keys {
		jsonValue := gjson.Get(string(payload), k)

		if jsonValue.Exists() {
			// jsonValue can exists but with no value
			if len(jsonValue.Str) == 0 {
				//return false, fmt.Errorf("found empty string in payload searching %s data", k)
				return false, nil
			}

			// Json data exists in payload, check whether matches any of the values
			for _, whenValue := range whenItem.Values {
				match, err := isDataMatches(whenValue, jsonValue.Str)

				if err != nil {
					return false, err
				}

				if match {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

func checkHttpHeadersCondition(whenItem WhenItem, httpHeaders http.Header) (bool, error) {
	for _, k := range whenItem.Keys {
		for headerName, headerValues := range httpHeaders {
			if strings.EqualFold(headerName, k) {
				// Iterate all values with one name (e.g. Content-Type)
				// Note: The same HTTP header could have multiple values
				for _, hdrVal := range headerValues {
					for _, whenValue := range whenItem.Values {
						match, err := isDataMatches(whenValue, hdrVal)

						if err != nil {
							return false, err
						}

						if match {
							return true, nil
						}
					}
				}
			}
		}
	}

	return false, nil
}

func checkQueryParamsCondition(whenItem WhenItem, queryParams url.Values) (bool, error) {
	for _, k := range whenItem.Keys {
		// Note: Query values are a slice but we use only the fist one, so query params
		//       should be unique
		for paramKey, paramValues := range queryParams {
			if strings.EqualFold(paramKey, k) {
				// Found http query param, check first value against all values
				// configured in when clause
				for _, whenValue := range whenItem.Values {
					match, err := isDataMatches(whenValue, paramValues[0])

					if err != nil {
						return false, err
					}

					if match {
						return true, nil
					}
				}
			}
		}
	}

	return false, nil
}

// sliceContains checks if a string is present in a slice
func sliceContains(str string, s []string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func isValidPipeline(pipelineName string) bool {
	var valid bool

	for _, item := range configuration.Pipelines {
		if item.Name == pipelineName {
			valid = true
			break
		}
	}

	return valid
}

func checkWorkspacesConfig(workspaces []Workspace) error {
	for _, w := range workspaces {

		if len(w.Name) == 0 {
			return errors.New("found an empty workspace name")
		}

		if len(w.Type) == 0 {
			return errors.New("found an empty workspace type")
		}

		// Check type
		if !sliceContains(w.Type, workspacesTypes) {
			return errors.New("workspace type unknown")
		}
	}

	return nil
}

func getkWorkspacesData(pipelineIdx int, workspaces []Workspace) error {
	for i, w := range workspaces {
		switch strings.ToLower(w.Type) {
		case strings.ToLower(EmptyDirType):
			emptyDirData := make(map[string]string)

			emptyDirData["template"] = fmt.Sprintf(`- name: %s
  emptyDir: {}`, w.Name)

			configuration.Pipelines[pipelineIdx].Workspaces[i].Data = emptyDirData
		case strings.ToLower(PersistentVolumeClaimType), strings.ToLower(VolumeClaimTemplateType):
			// Get ConfigMap
			configmap, err := k8s.GetConfigMap(w.Name, os.Getenv("POD_NAMESPACE"))

			if err != nil {
				return err
			}

			configuration.Pipelines[pipelineIdx].Workspaces[i].Data = configmap.Data
		case strings.ToLower(ConfigmapType):
			configMapData := make(map[string]string)

			configMapData["template"] = fmt.Sprintf(`- name: %s
  configmap: 
    name: %s`, w.Name, w.Name)

			configuration.Pipelines[pipelineIdx].Workspaces[i].Data = configMapData
		case strings.ToLower(SecretType):
			secretData := make(map[string]string)

			secretData["template"] = fmt.Sprintf(`- name: %s
  secret: 
    name: %s`, w.Name, w.Name)

			configuration.Pipelines[pipelineIdx].Workspaces[i].Data = secretData
		}
	}

	return nil
}

func loadGithubIps() ([]string, error) {
	var ips []string

	// Get GitHub IPs
	client := http.Client{
		Timeout: time.Duration(10 * time.Second),
	}

	request, err := http.NewRequest("GET", "https://api.github.com/meta", nil)
	request.Header.Set("Content-type", "application/json")

	if err != nil {
		return ips, err
	}

	resp, err := client.Do(request)

	if err != nil {
		return ips, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ips, fmt.Errorf("problems getting github metadata, status: %s", resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return ips, err
	}

	var target map[string]interface{}

	err = json.Unmarshal(body, &target)

	if err != nil {
		return ips, err
	}

	_, ok := target["hooks"]

	if !ok {
		return ips, fmt.Errorf("hooks field not found in metadata from github")
	}

	ipsTmp := target["hooks"].([]interface{})

	if len(ipsTmp) == 0 {
		return ips, fmt.Errorf("hooks field is empty in metadata from github")
	}

	for i := range ipsTmp {
		ips = append(ips, ipsTmp[i].(string))
	}

	return ips, nil
}

func parseWhen(when []WhenItem) error {
	for _, whenItem := range when {
		if len(whenItem.Kind) == 0 {
			return errors.New("kind field is empty in when clause")
		}

		if len(whenItem.Keys) == 0 {
			return errors.New("keys field is empty in when clause")
		}

		if len(whenItem.Values) == 0 {
			return errors.New("values field is empty in when clause")
		}

		// Check kind
		if !sliceContains(strings.ToLower(whenItem.Kind), whenKinds) {
			return fmt.Errorf("kind field (%s) unknown in when clause", whenItem.Kind)
		}

		// Check keys
		for i := range whenItem.Keys {
			if len(whenItem.Keys[i]) == 0 {
				return fmt.Errorf("found an empty key in when clause in kind %s", whenItem.Kind)
			}
		}

		// Check values
		for _, v := range whenItem.Values {
			if len(v.Operator) == 0 {
				return fmt.Errorf("found an empty operator in when clause in kind %s", whenItem.Kind)
			} else {
				// Check if opeerator is correct
				if !sliceContains(strings.ToLower(v.Operator), valuesOperators) {
					return fmt.Errorf("found a unknown operator in when clause in kind %s", whenItem.Kind)
				}
			}

			if len(v.Data) == 0 {
				return fmt.Errorf("found an empty data in when clause in kind %s", whenItem.Kind)
			}
		}
	}

	return nil
}

func isDataMatches(valueItem ValueItem, value string) (bool, error) {
	switch strings.ToLower(valueItem.Operator) {
	case valueOperatorEqual:
		if valueItem.Data == value {
			return true, nil
		}
	case valueOperatorNotEqual:
		if valueItem.Data != value {
			return true, nil
		}
	case valueOperatorContains:
		match, err := regexp.MatchString(valueItem.Data, value)

		if err != nil {
			return false, err
		}

		if match {
			return true, nil
		}
	case valueOperatorNotContains:
		match, err := regexp.MatchString(valueItem.Data, value)

		if err != nil {
			return false, err
		}

		if !match {
			return true, nil
		}
	}

	return false, nil
}
