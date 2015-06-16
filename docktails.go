package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/fsouza/go-dockerclient"
)

var (
	red   = "\x1b[0;31m"
	green = "\x1b[0;32m"
	blue  = "\x1b[0;34m"
	bold  = "\x1b[1m"
	reset = "\x1b[0m"
)

type prefixWriter struct {
	w      io.Writer
	prefix string
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.w.Write([]byte(p.prefix))
	return p.w.Write(b)
}

func startDockerLogs(dockerClient *docker.Client, containerID string) {
	container, err := dockerClient.InspectContainer(containerID)
	if err != nil {
		log.Fatalf("unable to get container: %s", err)
	}

	if !container.State.Running {
		return
	}

	if container.Config.Tty {
		return
	}

	log.Printf("starting docker logs for %s %s",
		container.ID[:12], container.Config.Image)

	name := strings.Trim(container.Name, "/")
	logOpts := docker.LogsOptions{
		Container:    containerID,
		Follow:       true,
		Tail:         "0",
		Stdout:       true,
		Stderr:       true,
		OutputStream: &prefixWriter{os.Stdout, green + name + reset + "  "},
		ErrorStream:  &prefixWriter{os.Stderr, red + name + reset + "  "},
	}

	err = dockerClient.Logs(logOpts)
	if err != nil {
		log.Fatalf("unable to start docker logs: %s", err)
	}
}

func main() {
	log.SetFlags(0)
	log.SetPrefix(blue + "docktails" + reset + "  ")

	dockerHost := os.Getenv("DOCKER_HOST")

	certPath := os.Getenv("DOCKER_CERT_PATH")
	ca := fmt.Sprintf("%s/ca.pem", certPath)
	cert := fmt.Sprintf("%s/cert.pem", certPath)
	key := fmt.Sprintf("%s/key.pem", certPath)

	dockerClient, err := docker.NewTLSClient(dockerHost, cert, key, ca)
	if err != nil {
		log.Fatalf("Error connecting to docker host: %s", err)
	}

	log.Printf("Connected to docker host: %s", dockerHost)

	// Tail logs on all existing containers that are running
	containers, err := dockerClient.ListContainers(docker.ListContainersOptions{All: true})
	for _, c := range containers {
		go startDockerLogs(dockerClient, c.ID)
	}

	// Listen for new containers to be started, and tail logs on those
	eventChan := make(chan *docker.APIEvents)
	dockerClient.AddEventListener(eventChan)
	for event := range eventChan {
		log.Printf("docker event %s%s%s %s container=%s", bold, reset, event.Status, event.From, event.ID[:12])

		switch event.Status {
		case "start":
			go startDockerLogs(dockerClient, event.ID)
		}
	}
}
