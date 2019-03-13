package scale

import (
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/seatgeek/nomad-helper/nomad"
	"github.com/seatgeek/nomad-helper/structs"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

var mu sync.Mutex

func ExportCommand(file string) error {
	log.Info("Reading jobs from Nomad")

	client, err := nomad.NewNomadClient()
	if err != nil {
		return err
	}

	info := make(map[string]string)
	info["nomad_addr"] = os.Getenv("NOMAD_ADDR")
	info["exported_at"] = time.Now().UTC().Format(time.RFC1123Z)
	info["exported_by"] = os.Getenv("USER")

	state := &structs.NomadState{
		Info: info,
		Jobs: make(map[string]structs.JobInfo),
	}

	var wg sync.WaitGroup
	workCh := make(chan *api.JobListStub, 10)

	go worker(&wg, client, state, workCh)
	go worker(&wg, client, state, workCh)
	go worker(&wg, client, state, workCh)
	go worker(&wg, client, state, workCh)
	go worker(&wg, client, state, workCh)
	go worker(&wg, client, state, workCh)
	go worker(&wg, client, state, workCh)

	jobStubs, _, err := client.Jobs().List(&api.QueryOptions{})
	if err != nil {
		return err
	}
	for _, jobStub := range jobStubs {
		wg.Add(1)
		workCh <- jobStub
	}

	wg.Wait()
	close(workCh)

	bytes, err := yaml.Marshal(state)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(file, bytes, 0644)
	if err != nil {
		return err
	}

	log.Info("Nomad state was successfully written out")
	return nil
}

func worker(wg *sync.WaitGroup, client *api.Client, state *structs.NomadState, ch <-chan *api.JobListStub) {
	for {
		jobStub, ok := <-ch
		if !ok {
			return
		}

		logger := log.WithField("job", jobStub.Name)
		logger.Debugf("Processing job")

		if strings.Contains(jobStub.ID, "/periodic-") {
			logger.Debugf("Skipping - periodic job")
			wg.Done()
			continue
		}

		if strings.Contains(jobStub.ID, "/dispatch-") {
			logger.Debugf("Skipping - paramertized job")
			wg.Done()
			continue
		}

		if jobStub.Type == "system" {
			logger.Debugf("Skipping - system job")
			wg.Done()
			continue
		}

		job, _, err := client.Jobs().Info(jobStub.Name, &api.QueryOptions{})
		if err != nil {
			logger.Errorf("Could not fetch job")
			wg.Done()
			continue
		}

		if len(job.Meta) > 0 {
			if _, present := job.Meta["seatgeek.inject_dynamic_values"]; present {
				logger.Warnf("Skipping - inject_dynamic_values")
				wg.Done()
				continue
			}
		}

		jobState := structs.JobInfo{
			Type:   *job.Type,
			Groups: make(map[string]structs.Details),
		}

		if cronitorID, ok := job.Meta["cronitor.prod.id"]; ok {
			jobState.CronitorID = cronitorID
		}

		if description, ok := job.Meta["description"]; ok {
			jobState.Description = description
		}

		if project, ok := job.Meta["name"]; ok {
			jobState.Project = project
		}

		if job.IsPeriodic() {
			jobState.Cron = *job.Periodic.Spec
		}

		serviceCount := 0
		for _, group := range job.TaskGroups {
			logger.WithField("group", *group.Name).Infof("scale = %d", *group.Count)

			details := structs.Details{
				Count:    *group.Count,
				Services: make([]string, 0),
			}

			for _, task := range group.Tasks {
				for _, service := range task.Services {
					details.Services = append(details.Services, service.Name)
				}
			}

			serviceCount = serviceCount + len(details.Services)

			if job.IsPeriodic() {
				details.MaintenanceCount = 0
			} else if job.IsParameterized() {
				details.MaintenanceCount = 0
			} else if serviceCount == 0 {
				details.MaintenanceCount = 0
			} else {
				details.MaintenanceCount = *group.Count
			}

			jobState.Groups[*group.Name] = details
		}

		if job.IsPeriodic() {
			jobState.InternalType = "periodic"
		} else if job.IsParameterized() {
			jobState.InternalType = "parameterized"
		} else if serviceCount == 0 {
			jobState.InternalType = "background-worker"
		} else {
			jobState.InternalType = "http-service"
		}

		mu.Lock()
		state.Jobs[*job.ID] = jobState
		mu.Unlock()

		wg.Done()
	}
}
