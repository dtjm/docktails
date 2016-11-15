package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
)

const version = "1.1.3"

var (
	red    = "\x1b[0;31m"
	green  = "\x1b[0;32m"
	brown  = "\x1b[0;33m"
	blue   = "\x1b[0;34m"
	purple = "\x1b[0;35m"
	cyan   = "\x1b[0;36m"

	boldBlue = "\x1b[1;34m"
	bold     = "\x1b[1m"
	reset    = "\x1b[0m"

	colors     = []string{red, green, brown, blue, purple, cyan}
	colorIndex = 0
)

func nextColor() string {
	colorIndex++
	if colorIndex == len(colors) {
		colorIndex = 0
	}
	return colors[colorIndex]
}

type prefixWriter struct {
	w          io.Writer
	prefix     string
	prettyJSON bool
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	numBytes := len(b)
	if numBytes == 0 {
		return 0, nil
	}

	p.w.Write([]byte(p.prefix))

	if p.prettyJSON {
		// Check if there is a JSON string in there
		firstBracketIndex := bytes.Index(b, []byte{'{'})
		lastBracketIndex := bytes.LastIndex(b, []byte{'}'})
		if firstBracketIndex != -1 && lastBracketIndex != -1 && lastBracketIndex > firstBracketIndex {
			var v interface{}
			jsonBytes := b[firstBracketIndex : lastBracketIndex+1]
			var input bytes.Buffer
			dec := json.NewDecoder(&input)
			dec.UseNumber()
			input.Write(jsonBytes)
			err := dec.Decode(&v)
			if err == nil {
				indentedBytes, err := json.MarshalIndent(v, "", "    ")
				if err == nil {
					b = bytes.Join([][]byte{b[:firstBracketIndex], indentedBytes, b[lastBracketIndex+1:]}, []byte{})
				}
			}
		}

		numLines := bytes.Count(b, []byte{'\n'})
		b = bytes.Replace(b, []byte{'\n'}, append([]byte{'\n'}, p.prefix...), numLines-1)
	}

	p.w.Write(b)
	return numBytes, nil
}

func retryConnect(eventChan chan *docker.APIEvents) *docker.Client {

	dockerHost := os.Getenv("DOCKER_HOST")

	certPath := os.Getenv("DOCKER_CERT_PATH")
	ca := fmt.Sprintf("%s/ca.pem", certPath)
	cert := fmt.Sprintf("%s/cert.pem", certPath)
	key := fmt.Sprintf("%s/key.pem", certPath)

	log.Printf("connecting to %s", dockerHost)

	for {
		var (
			dockerClient *docker.Client
			err          error
		)

		if dockerHost == "" {
			dockerClient, err = docker.NewClientFromEnv()
			if err != nil {
				log.Printf("error connecting to docker host, retrying in 5s: %s", err)
				continue
			}
		} else {
			dockerClient, err = docker.NewTLSClient(dockerHost, cert, key, ca)
			if err != nil {
				log.Printf("error connecting to docker host, retrying in 5s: %s", err)
				time.Sleep(5 * time.Second)
				continue
			}
		}

		err = dockerClient.Ping()
		if err != nil {
			log.Printf("error pinging docker host, retrying in 5s: %s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		log.Print("connection succeeded")
		dockerClient.AddEventListener(eventChan)
		return dockerClient
	}
}

func startDockerLogs(dockerClient *docker.Client, containerID string, prettyJSON bool) {
	container, err := dockerClient.InspectContainer(containerID)
	if err != nil {
		log.Printf("unable to get container: %s", err)
		return
	}

	if !container.State.Running {
		return
	}

	if container.Config.Tty {
		return
	}

	log.Printf("starting docker logs for %s %s",
		container.ID[:12], container.Config.Image)

	color := nextColor()

	name := strings.Trim(container.Name, "/")
	logOpts := docker.LogsOptions{
		Container:    containerID,
		Follow:       true,
		Tail:         "0",
		Stdout:       true,
		Stderr:       true,
		OutputStream: &prefixWriter{os.Stdout, color + name + reset + "  ", prettyJSON},
		ErrorStream:  &prefixWriter{os.Stderr, color + name + reset + "  ", prettyJSON},
	}

	err = dockerClient.Logs(logOpts)
	if err != nil {
		log.Printf("unable to start docker logs for %s: %s", container.Config.Image, err)
	}
}

func main() {
	log.SetFlags(0)
	log.SetPrefix(boldBlue + "docktails" + reset + "  ")
	var (
		eventChan    chan *docker.APIEvents
		prettyJSON   bool
		prefixMatch  string
		printVersion bool
	)

	flag.BoolVar(&prettyJSON, "json", true, "Pretty-print JSON")
	flag.StringVar(&prefixMatch, "prefix", "", "Prefix to match for container names")
	flag.BoolVar(&printVersion, "version", false, "Print version and exit")
	flag.Parse()

	if printVersion {
		fmt.Println("docktails " + version + " " + runtime.Version())
		os.Exit(0)
	}
	log.Printf("starting version %s", version)

START:
	eventChan = make(chan *docker.APIEvents)
	dockerClient := retryConnect(eventChan)

	// Tail logs on all existing containers that are running
	for {
		log.Printf("getting container list...")
		containers, err := dockerClient.ListContainers(docker.ListContainersOptions{All: true})
		if err != nil {
			log.Printf("error listing containers: %q; retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(containers) == 0 {
			log.Print("no containers running")
		}

		log.Printf("starting logs")
		for _, c := range containers {
		INNER:
			for _, name := range c.Names {
				if strings.HasPrefix(name, "/"+prefixMatch) {
					go startDockerLogs(dockerClient, c.ID, prettyJSON)
					break INNER
				}
			}
		}
		break
	}

	dockerLog := log.New(os.Stdout, boldBlue+"docker"+reset+"  ", 0)

	// Listen for new containers to be started, and tail logs on those
	for event := range eventChan {
		containerID := event.ID
		if len(containerID) > 12 {
			containerID = containerID[:12]
		}

		dockerLog.Printf("event %s%s%s %s container=%s", bold, event.Status, reset, event.From, containerID)

		container, err := dockerClient.InspectContainer(containerID)
		if err != nil {
			log.Printf("error inspecting container=%s", containerID)
			continue
		}

		switch event.Status {
		case "start":
			if strings.HasPrefix(container.Name, "/"+prefixMatch) {
				go startDockerLogs(dockerClient, event.ID, prettyJSON)
			}
		}
	}

	log.Print("event channel closed, probably lost connection to Docker host; retrying...")
	goto START
}
