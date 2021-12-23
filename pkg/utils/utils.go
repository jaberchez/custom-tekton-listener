package utils

import (
	"errors"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	//"github.com/rs/xid"
	"github.com/sirupsen/logrus"
	"github.com/teris-io/shortid"

	"github.com/jaberchez/custom-tekton-listener/pkg/config"
)

var (
	logger             *logrus.Logger
	pipelineRunIdField string
)

func init() {
	logger = &logrus.Logger{
		Out:   os.Stdout,
		Level: logrus.DebugLevel,
		Formatter: &logrus.TextFormatter{
			DisableColors:   true,
			TimestampFormat: "2006-01-02 15:04:05",
			FullTimestamp:   true,
		},
	}

	pipelineRunIdField = ""
}

func Log(severity string, message string) {
	switch strings.ToLower(severity) {
	case "info":
		if len(pipelineRunIdField) > 0 {
			logger.WithFields(logrus.Fields{
				"pipelineRunId": pipelineRunIdField,
			}).Info(message)

		} else {
			logger.Info(message)
		}
	case "warning":
		if len(pipelineRunIdField) > 0 {
			logger.WithFields(logrus.Fields{
				"pipelineRunId": pipelineRunIdField,
			}).Warn(message)

		} else {
			logger.Warn(message)
		}
	case "error":
		if len(pipelineRunIdField) > 0 {
			logger.WithFields(logrus.Fields{
				"pipelineRunId": pipelineRunIdField,
			}).Error(message)

		} else {
			logger.Error(message)
		}
	case "fatal":
		if len(pipelineRunIdField) > 0 {
			logger.WithFields(logrus.Fields{
				"pipelineRunId": pipelineRunIdField,
			}).Fatal(message)

		} else {
			logger.Fatal(message)
		}
	}
}

func SetPipelineRunIdFieldLog(pipelineRunId string) {
	pipelineRunIdField = pipelineRunId
}

func GetIpFromRequest(r *http.Request) (string, error) {
	// Check if the ip come from the header X-Forwarded-For
	ipSource := r.Header.Get("x-forwarded-for")

	if len(ipSource) > 0 {
		return ipSource, nil
	}

	if len(r.RemoteAddr) > 0 {
		idx := strings.LastIndex(r.RemoteAddr, ":")

		if idx < 0 {
			return r.RemoteAddr, nil
		}

		return r.RemoteAddr[0:idx], nil
	}

	return "", errors.New("IP from request not found")
}

func CheckGitHubIps(sourceIp string) (bool, error) {
	var allowed bool

	ips := config.GetGithubIps()

	// Check if source ip come from github
	ipSrc := net.ParseIP(sourceIp)

	for i := range ips {
		_, ipNet, err := net.ParseCIDR(ips[i])

		if err != nil {
			return false, err
		}

		if ipNet.Contains(ipSrc) {
			allowed = true
			break
		}
	}

	return allowed, nil
}

// GenId generate a unique id for PipelineRun
// Note: Try to make sure it is unique because probably multiple instances of
//       this application are running
func GenId() (string, error) {
	const totalLen int = 12

	id, err := shortid.Generate()

	if err != nil {
		return "", err
	}

	id = strings.ReplaceAll(id, "_", "")
	id = strings.ReplaceAll(id, "-", "")
	id = strings.ToLower(id)

	// Same size for all ids
	if len(id) < totalLen {
		// Add until totalLen
		rand.Seed(time.Now().UnixNano())

		var letterRunes = []rune("abc0de1fg2hi3jkl4m5nop6qrs7tu8vwx9yz")
		b := make([]rune, totalLen-len(id))

		for i := range b {
			b[i] = letterRunes[rand.Intn(len(letterRunes))]
		}

		id += string(b)
	}

	if len(id) > totalLen {
		id = id[0:totalLen]
	}

	return id, nil

	//id := xid.New()
	//
	//return id.String()
}
