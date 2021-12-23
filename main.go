package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	githubv1 "github.com/jaberchez/custom-tekton-listener/pkg/api/v1/github"
	"github.com/jaberchez/custom-tekton-listener/pkg/config"
	"github.com/jaberchez/custom-tekton-listener/pkg/utils"
)

const (
	listenPort string = "8080"
)

var (
	checkGithubIps bool
	isServerReady  bool
)

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Listener is up and running")
}

func startupHealthCheck(w http.ResponseWriter, r *http.Request) {
	if isServerReady {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Listener is up and running")
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Listener is not ready")
	}
}

func gitHubListenerV1(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	// Check source ip
	if checkGithubIps {
		sourceIp, err := utils.GetIpFromRequest(r)

		if err != nil {
			utils.Log("ERROR", err.Error())

			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "some internal error ocurred")

			return
		}

		allowed, err := utils.CheckGitHubIps(sourceIp)

		if err != nil {
			utils.Log("ERROR", err.Error())

			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "some internal error ocurred")

			return
		}

		if !allowed {
			utils.Log("ERROR", fmt.Sprintf("source IP %s not allowed", sourceIp))

			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, "source ip not allowed")

			return
		}
	}

	// Get Github event from Header
	githubEvent, err := getGithubEvent(r)

	if err != nil {
		utils.Log("ERROR", err.Error())

		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "some internal error ocurred")

		return
	}

	if githubEvent == "ping" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "pong at %s", time.Now().Format("2006-01-02 15:04:05.000"))

		return
	}

	// Create unique id for this PipelineRun
	id, err := utils.GenId()

	if err != nil {
		utils.Log("ERROR", fmt.Sprintf("unable to create pipelinerun id: %s ", err.Error()))

		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "some internal error ocurred")

		return
	}

	// Set id for logs
	utils.SetPipelineRunIdFieldLog(id)

	// We should respond as quickly as we can for timeout issues
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Queued request id %s at %s\n", id, time.Now().Format("2006-01-02 15:04:05.000"))

	// Read body
	body, err := ioutil.ReadAll(r.Body)

	if err != nil {
		utils.Log("ERROR", fmt.Sprintf("cannot read payload: %s", err.Error()))
		return
	}

	// Handle the request in a go routine
	go func() {
		//utils.SetPipelineRunIdFieldLog(id)

		gitHub := &githubv1.GitHub{
			ID:          id,
			HttpRequest: r,
			CheckIps:    checkGithubIps,
			Payload:     body,
			GithubEvent: githubEvent,
		}

		// Process the request
		gitHub.HandleRequest()
	}()
}

func getGithubEvent(r *http.Request) (string, error) {
	// Get X-GitHub-Event header
	event := r.Header.Get("X-GitHub-Event")

	if len(event) == 0 {
		return "", errors.New("X-GitHub-Event header not found")
	}

	return event, nil
}

func main() {
	podNamespace := os.Getenv("POD_NAMESPACE")
	port := os.Getenv("LISTEN_PORT")
	checkGithubIpsEnv := os.Getenv("CHECK_GITHUB_IPS")
	pipelinesNamespace := os.Getenv("PIPELINES_NAMESPACE")

	if len(podNamespace) == 0 {
		utils.Log("FATAL", "unable to find the enviroment variable POD_NAMESPACE")
	}

	if len(port) == 0 {
		port = listenPort
	}

	if len(checkGithubIpsEnv) == 0 {
		// Default true
		checkGithubIps = true
	} else {
		checkGithubIpsEnv = strings.ToLower(checkGithubIpsEnv)

		if checkGithubIpsEnv != "true" && checkGithubIpsEnv != "false" {
			checkGithubIps = true
		} else {
			checkGithubIps = checkGithubIpsEnv == "true"
		}
	}

	if len(pipelinesNamespace) == 0 {
		// Pipelines namespace not found, using the same as pod
		os.Setenv("PIPELINES_NAMESPACE", podNamespace)
	}

	// Load configuration
	err := config.LoadConfig(checkGithubIps)

	if err != nil {
		utils.Log("FATAL", fmt.Sprintf("unable to load app configuration: %s", err.Error()))
	}

	// Parse configuration
	err = config.ParseConfig()

	if err != nil {
		utils.Log("FATAL", err.Error())
	}

	r := mux.NewRouter()

	r.HandleFunc("/api/v1/github", gitHubListenerV1).Methods("POST") // Only POST allowed
	r.HandleFunc("/startup", startupHealthCheck)
	r.HandleFunc("/liveness", healthCheck)
	r.HandleFunc("/readiness", healthCheck)
	r.HandleFunc("/", healthCheck)

	srv := &http.Server{
		Handler:      r,
		Addr:         fmt.Sprintf(":%s", listenPort),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	utils.Log("INFO", fmt.Sprintf("server listening on port %s", listenPort))

	isServerReady = true

	err = srv.ListenAndServe()

	if err != nil {
		utils.Log("FATAL", err.Error())
	}
}
