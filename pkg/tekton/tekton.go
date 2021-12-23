package tekton

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"encoding/base64"
	"encoding/json"

	"github.com/Masterminds/sprig"
	"github.com/jaberchez/custom-tekton-listener/pkg/config"
	"github.com/jaberchez/custom-tekton-listener/pkg/k8s"
)

const (
	tektonApiGroup   string = "tekton.dev"
	tektonApiVersion string = "v1beta1"
	pipelineRunKind  string = "PipelineRun"
)

var workspacesTemplate string = `{{ define "workspaces" }}
{{- range  .Workspaces }}
{{- range $key, $value := .Data}}
{{ $value | indent 4}}
{{- end }}
{{- end }}
{{- end }}
`

var pipelineRunTemplate string = fmt.Sprintf(`apiVersion: %s/%s
kind: %s
metadata:
  name: {{ .Prefix }}-{{ .ID }}
  namespace: {{ .Namespace }}
  annotations:
    {{- range $key, $value := .Annotations }}
    {{ $key }}: "{{ $value }}"
    {{- end }}
  labels:
    {{- range $key, $value := .Labels }}
    {{ $key }}: "{{ $value }}"
    {{- end }}
spec:
  {{- $length := len .ServiceAccount }} {{ if gt $length 0 }}
  serviceAccountName: {{ .ServiceAccount }}
  {{- end }}
  params:
    {{- range $key, $value := .Params }}
    - name: {{ $key }}
      value: "{{ $value }}"
	 {{- end }}
  pipelineRef:
    name: {{ .PipelineName }}
  {{- $length := len .Workspaces }} {{ if gt $length 0 }}	
  workspaces:
  {{- template "workspaces" . }}
  {{- end }}
  {{- $length := len .Resources }} {{ if gt $length 0 }}	
  resources:
  {{- range .Resources }}
    - name: {{ .Name }}
      resourceRef:
        name: {{ .ResourceRef }}
  {{- end }}
  {{- end }}
`, tektonApiGroup, tektonApiVersion, pipelineRunKind)

type PipelineRun struct {
	ID             string
	PipelineName   string
	Namespace      string
	Prefix         string
	GitHubPayload  []byte
	GitHubEvent    string
	Params         map[string]string
	ExtraParams    map[string]string
	Workspaces     []config.Workspace
	Resources      []config.Resource
	Labels         map[string]string
	Annotations    map[string]string
	ServiceAccount string
}

func (p *PipelineRun) Start() error {
	tplStr, err := p.renderTemplate()

	if err != nil {
		return err
	}

	//filename := fmt.Sprintf("/tmp/workspace-template-%s.yaml", p.ID)
	//
	//_ = createTemplateFile(filename, tplStr)

	// Create PipelineRun in Kubernetes
	return k8s.CreateObject(tektonApiGroup, tektonApiVersion, pipelineRunKind, os.Getenv("PIPELINES_NAMESPACE"), tplStr)
}

//func createTemplateFile(filename string, fileData string) error {
//	d := []byte(fileData)
//
//	return os.WriteFile(filename, d, 0644)
//}

func (p *PipelineRun) renderTemplate() (string, error) {
	p.Namespace = os.Getenv("PIPELINES_NAMESPACE")

	// Set labels
	labels := make(map[string]string)

	labels["pipelinerun-id"] = p.ID

	p.Labels = labels

	// Set annotations
	annotations := make(map[string]string)

	annotations["pipelinerun-created-by"] = filepath.Base(os.Args[0])
	annotations["pipelinerun-created-at"] = time.Now().Format("2006-01-02_15-04-05.000")
	annotations["pipeline-name"] = p.PipelineName

	p.Annotations = annotations

	// Set params
	params := make(map[string]string)

	params["payloadBase64"] = base64.StdEncoding.EncodeToString(p.GitHubPayload) // Payload encoded in base64
	params["event"] = p.GitHubEvent
	params["pipelineRunId"] = p.ID

	// Add extra params
	for k, v := range p.ExtraParams {
		// Don't provide pipeline and prefix as parameter (useful)
		if strings.EqualFold(k, "pipeline") || strings.EqualFold(k, "prefix") {
			continue
		}

		params[k] = v
	}

	p.Params = params

	var tpl bytes.Buffer

	t := template.Must(template.New("base").Funcs(sprig.FuncMap()).Parse(workspacesTemplate))
	t = template.Must(t.Parse(pipelineRunTemplate))

	err := t.Execute(&tpl, p)

	if err != nil {
		return "", err
	}

	s := tpl.String()
	s = strings.ReplaceAll(s, "&#34;", "\\\"") // Replace " for \"
	s = strings.ReplaceAll(s, "&#39;", "\\'")  // Repace ' for \'

	return s, nil
}

func convertMapToString(m map[string]string) (string, error) {
	jsonString, err := json.Marshal(m)

	if err != nil {
		return "", err
	}

	return string(jsonString), nil
}

//func (p *PipelineRun) getDataFromGithubPayload(payload map[string]interface{}, dataName string) (string, error) {
//	if dataName == "cloneUrl" {
//		if !existsFieldFromMap(payload, "repository") {
//			return "", errors.New("field repository not found in payload")
//		}
//
//		repo := payload["repository"].(map[string]interface{})
//
//		if !existsFieldFromMap(repo, "clone_url") {
//			return "", errors.New("field repository.clone_url not found in payload")
//		}
//
//		return repo["clone_url"].(string), nil
//	} else if dataName == "sshUrl" {
//		if !existsFieldFromMap(payload, "repository") {
//			return "", errors.New("field repository not found in payload")
//		}
//
//		repo := payload["repository"].(map[string]interface{})
//
//		if !existsFieldFromMap(repo, "ssh_url") {
//			return "", errors.New("field repository.ssh_url not found in payload")
//		}
//
//		return repo["ssh_url"].(string), nil
//	} else if dataName == "branch" {
//		if !existsFieldFromMap(payload, "ref") {
//			return "", errors.New("field ref not found in payload")
//		}
//
//		pos := strings.LastIndex(payload["ref"].(string), "/")
//
//		if pos < 0 {
//			return "", errors.New("uname to get branch name from refs")
//		}
//
//		branch := payload["ref"].(string)
//		branch = branch[pos+1:]
//
//		return branch, nil
//	} else if dataName == "commitId" {
//		if !existsFieldFromMap(payload, "head_commit") {
//			return "", errors.New("field head_commit not found in payload")
//		}
//
//		repo := payload["head_commit"].(map[string]interface{})
//
//		if !existsFieldFromMap(repo, "id") {
//			return "", errors.New("field head_commit.id not found in payload")
//		}
//
//		return repo["id"].(string), nil
//	}
//
//	return "", nil
//}
//
//func existsFieldFromMap(m map[string]interface{}, field string) bool {
//	_, ok := m[field]
//
//	return ok
//}
