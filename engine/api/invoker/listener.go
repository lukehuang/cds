package invoker

import (
	"time"

	"github.com/fsamin/go-dump"
	"github.com/go-gorp/gorp"

	"github.com/ovh/cds/engine/api/cache"
	"github.com/ovh/cds/engine/api/pipeline"
	"github.com/ovh/cds/engine/api/project"
	"github.com/ovh/cds/engine/log"
	"github.com/ovh/cds/sdk"
)

// Listen si the main goroutine
func Listen(DBFunc func() gorp.SqlExecutor) {
	for {
		time.Sleep(10 * time.Millisecond)
		//Check if CDS is in maintenance mode
		var m bool
		cache.Get("maintenance", &m)
		if m {
			log.Warning("⚠ CDS maintenance in ON")
			time.Sleep(30 * time.Second)
		}

		var e sdk.PipelineInvokeEvent
		cache.Dequeue("pipeline_invoke_events", &e)

		db := DBFunc()
		if db != nil && !m {
			if err := proces(db, e); err != nil {
				log.Critical("invoker.Listen> err while processing %s : %v", err, e)
			}
			continue
		} else {
			cache.Enqueue("pipeline_invoke_events", e)
		}
	}
}

func proces(db gorp.SqlExecutor, e sdk.PipelineInvokeEvent) error {
	if e.ListenerUUID == "" {
		return sdk.ErrInvalidEvent
	}
	e.Date = time.Now()

	// Load listener definition to know application, pipeline and environment
	l, err := Load(db, e.ListenerUUID)
	if err != nil {
		return err
	}

	// Transform paylogs to cds args
	mapArgs, err := dump.ToMap(e.Payload)
	if err != nil {
		return err
	}
	args := sdk.ParametersFromMap(mapArgs)

	// Prepare PipelineBuildTrigger object
	t := sdk.PipelineBuildTrigger{}
	for _, arg := range args {
		switch arg.Name {
		case "git.hash":
			t.VCSChangesHash = arg.Value
		case "git.branch":
			t.VCSChangesBranch = arg.Value
		case "git.author":
			t.VCSChangesAuthor = arg.Value
		}
	}

	// Load the project
	proj, err := project.Load(db, l.Application.ProjectKey, nil)
	if err != nil {
		return err
	}

	//Trigger the pipeline
	if err := pipeline.Run(db, proj, &l.Pipeline, &l.Application, &l.Environment, sdk.PipelineBuildTrigger{}, args...); err != nil {
		return err
	}

	return nil
}