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

func main() {
	log.SetFlags(0)
	log.SetPrefix(blue + "dockertail" + reset + "  ")

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

	baselogOpts := docker.LogsOptions{
		Follow: true,
		Tail:   "0",
		Stdout: true,
		Stderr: true,
	}

	containers, err := dockerClient.ListContainers(docker.ListContainersOptions{All: true})
	for _, c := range containers {
		if strings.HasPrefix(c.Status, "Exited") {
			continue
		}

		log.Printf("Starting docker logs for %s image=%s status=%s", c.ID[:12], c.Image, c.Status)
		logOpts := baselogOpts
		logOpts.Container = c.ID
		stdoutWriter := prefixWriter{os.Stdout, green + c.Names[0] + reset + "  "}
		stderrWriter := prefixWriter{os.Stderr, red + c.Names[0] + reset + "  "}
		logOpts.OutputStream = &stdoutWriter
		logOpts.ErrorStream = &stderrWriter

		go func() {
			err := dockerClient.Logs(logOpts)
			if err != nil {
				log.Fatalf("unable to start docker logs: %s", err)
			}
		}()
	}

	eventChan := make(chan *docker.APIEvents)

	dockerClient.AddEventListener(eventChan)

	for event := range eventChan {
		log.Printf("%s %s container=%s", event.From, event.Status, event.ID)

		switch event.Status {
		case "start":
			logOpts := baselogOpts
			logOpts.Container = event.ID

			container, err := dockerClient.InspectContainer(event.ID)
			if err != nil {
				log.Fatalf("unable to get container: %s", err)
			}

			stdoutWriter := prefixWriter{os.Stdout, green + container.Name + reset + "  "}
			stderrWriter := prefixWriter{os.Stderr, red + container.Name + reset + "  "}
			logOpts.OutputStream = &stdoutWriter
			logOpts.ErrorStream = &stderrWriter
			go func() {
				err := dockerClient.Logs(logOpts)
				if err != nil {
					log.Fatalf("unable to start docker logs: %s", err)
				}
			}()
		}
	}
}
