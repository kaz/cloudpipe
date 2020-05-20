package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"

	"cloud.google.com/go/logging"
	"github.com/urfave/cli/v2"
	"google.golang.org/genproto/googleapis/api/monitoredres"
)

func tailf(wg *sync.WaitGroup, r io.Reader, logger *logging.Logger, severity logging.Severity) {
	defer wg.Done()
	br := bufio.NewReader(r)

	for {
		line, _, err := br.ReadLine()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("br.ReadLine failed: %#v\n", err)
			}
			break
		}

		payload := string(line)
		if len(payload) == 0 {
			payload = " "
		}

		logger.Log(logging.Entry{
			Severity: severity,
			Payload:  payload,
		})
	}
}

func action(c *cli.Context) error {
	ctx := context.Background()
	client, err := logging.NewClient(ctx, c.String("project-id"))
	if err != nil {
		return fmt.Errorf("logging.NewClient failed: %w", err)
	}

	logger := client.Logger("cloudpipe", logging.CommonResource(&monitoredres.MonitoredResource{
		Type: "generic_task",
		Labels: map[string]string{
			"project_id": c.String("project-id"),
			"location":   c.String("location"),
			"namespace":  c.String("namespace"),
			"job":        c.String("job"),
			"task_id":    c.String("task-id"),
		},
	}))

	args := c.Args().Slice()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	wg := &sync.WaitGroup{}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("cmd.StdoutPipe failed: %w", err)
	}

	wg.Add(1)
	go tailf(wg, stdout, logger, logging.Info)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("cmd.StderrPipe failed: %w", err)
	}

	wg.Add(1)
	go tailf(wg, stderr, logger, logging.Error)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cmd.Start failed: %w", err)
	}

	wg.Wait()

	if err := logger.Flush(); err != nil {
		return fmt.Errorf("logger.Flush failed: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("cmd.Wait failed: %w", err)
	}

	return nil
}

func main() {
	app := &cli.App{
		Name:   "cloudpipe",
		Action: action,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "project-id, p",
				Usage:    "The identifier of the GCP project associated with this resource, such as \"my-project\".",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "location, l",
				Usage: "The GCP or AWS region in which data about the resource is stored. For example, \"us-east1-a\" (GCP) or \"aws:us-east-1a\" (AWS).",
			},
			&cli.StringFlag{
				Name:  "namespace, n",
				Usage: "A namespace identifier, such as a cluster name.",
			},
			&cli.StringFlag{
				Name:  "job, j",
				Usage: "An identifier for a grouping of related tasks, such as the name of a microservice or distributed batch job.",
			},
			&cli.StringFlag{
				Name:  "task-id, t",
				Usage: "A unique identifier for the task within the namespace and job, such as a replica index identifying the task within the job.",
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
