package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

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

func retryConnect(eventChan chan *docker.APIEvents) *docker.Client {

	dockerHost := os.Getenv("DOCKER_HOST")

	certPath := os.Getenv("DOCKER_CERT_PATH")
	ca := fmt.Sprintf("%s/ca.pem", certPath)
	cert := fmt.Sprintf("%s/cert.pem", certPath)
	key := fmt.Sprintf("%s/key.pem", certPath)

	log.Printf("connecting to %s", dockerHost)

	for {
		dockerClient, err := docker.NewTLSClient(dockerHost, cert, key, ca)
		if err != nil {
			log.Printf("error connecting to docker host, retrying in 5s: %s", err)
			time.Sleep(5 * time.Second)
			continue
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

func startDockerLogs(dockerClient *docker.Client, containerID string) {
	container, err := dockerClient.InspectContainer(containerID)
	if err != nil {
		log.Printf("unable to get container: %s", err)
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
		log.Printf("unable to start docker logs for %s: %s", container.Config.Image, err)
	}
}

func main() {
	log.SetFlags(0)
	log.SetPrefix(blue + "docktails" + reset + "  ")
	var eventChan chan *docker.APIEvents

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
			go startDockerLogs(dockerClient, c.ID)
		}
		break
	}

	// Listen for new containers to be started, and tail logs on those
	for event := range eventChan {
		containerID := event.ID
		if len(containerID) > 12 {
			containerID = containerID[:12]
		}
		log.Printf("docker event %s%s%s %s container=%s", bold, reset, event.Status, event.From, containerID)

		switch event.Status {
		case "start":
			go startDockerLogs(dockerClient, event.ID)
		}
	}

	log.Print("event channel closed, probably lost connection to Docker host; retrying...")
	goto START
}
