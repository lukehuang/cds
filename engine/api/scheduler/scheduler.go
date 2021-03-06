package scheduler

import (
	"fmt"
	"time"

	"github.com/go-gorp/gorp"
	"github.com/gorhill/cronexpr"

	"github.com/ovh/cds/sdk"
	"github.com/ovh/cds/sdk/log"
)

var schedulerStatus = "Not Running"

//Scheduler is the goroutine which compute date of next execution for pipeline scheduler
func Scheduler(DBFunc func() *gorp.DbMap) {
	for {
		time.Sleep(2 * time.Second)
		_, status, err := Run(DBFunc())

		if err != nil {
			log.Error("%s: %s", status, err)
		}
		schedulerStatus = status
	}
}

//Run is the core function of Scheduler goroutine
func Run(db *gorp.DbMap) ([]sdk.PipelineSchedulerExecution, string, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, "Run> Unable to start a transaction", err
	}
	defer tx.Rollback()

	//Starting with exclusive lock on the table
	if err := LockPipelineExecutions(tx); err != nil {
		return nil, "OK", nil
	}

	//Load unscheduled pipelines
	ps, err := LoadUnscheduledPipelines(tx)
	if err != nil {
		return nil, "Run> Unable to load unscheduled pipelines : %s", err
	}

	execs := []sdk.PipelineSchedulerExecution{}

	for i := range ps {
		//Skip disabled scheduler
		if ps[i].Disabled {
			continue
		}

		//Compute a new execution
		e, err := Next(tx, &ps[i])
		if err != nil {
			//Nothing to compute
			continue
		}
		//Insert it
		if err := InsertExecution(tx, e); err != nil {
			return nil, "Run> Unable to insert an execution : %s", err
		}
		execs = append(execs, *e)
	}

	if err := tx.Commit(); err != nil {
		return nil, "Run> Unable to commit a transaction : %s", err
	}

	return execs, "OK", nil
}

//Next Compute the next PipelineSchedulerExecution
func Next(db gorp.SqlExecutor, s *sdk.PipelineScheduler) (*sdk.PipelineSchedulerExecution, error) {
	cronExpr, err := cronexpr.Parse(s.Crontab)
	if err != nil {
		log.Warning("scheduler.Next> Unable to parse cronexpr for ID %d : %s", s.ID, err)
		return nil, err
	}
	exec, err := LoadLastExecution(db, s.ID)
	if err != nil {
		return nil, nil
	}

	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, err
	}

	if exec == nil {
		t := time.Now().In(loc)
		exec = &sdk.PipelineSchedulerExecution{
			Executed:      true,
			ExecutionDate: &t,
		}
	}

	if !exec.Executed {
		return nil, fmt.Errorf("Last execution %d not ran", s.ID)
	}
	nextTime := cronExpr.Next(exec.ExecutionDate.In(loc))
	e := &sdk.PipelineSchedulerExecution{
		ExecutionPlannedDate: nextTime,
		PipelineSchedulerID:  s.ID,
		Executed:             false,
	}
	return e, nil
}

// Status returns Event status
func Status() string {
	if schedulerStatus != "OK" {
		return "⚠ " + schedulerStatus
	}
	return schedulerStatus
}
