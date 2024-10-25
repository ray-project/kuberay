package v1alpha1

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/volcano"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/yunikorn"
)

func TestValidateBatchSchedulerConfig(t *testing.T) {
	type args struct {
		logger logr.Logger
		config Configuration
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "legacy option, enable-batch-scheduler=false",
			args: args{
				logger: testr.New(t),
				config: Configuration{
					EnableBatchScheduler: false,
				},
			},
			wantErr: false,
		},
		{
			name: "legacy option, enable-batch-scheduler=true",
			args: args{
				logger: testr.New(t),
				config: Configuration{
					EnableBatchScheduler: true,
				},
			},
			wantErr: false,
		},
		{
			name: "valid option, batch-scheduler=yunikorn",
			args: args{
				logger: testr.New(t),
				config: Configuration{
					BatchScheduler: yunikorn.GetPluginName(),
				},
			},
			wantErr: false,
		},
		{
			name: "valid option, batch-scheduler=volcano",
			args: args{
				logger: testr.New(t),
				config: Configuration{
					BatchScheduler: volcano.GetPluginName(),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid option, invalid scheduler name",
			args: args{
				logger: testr.New(t),
				config: Configuration{
					EnableBatchScheduler: false,
					BatchScheduler:       "unknown-scheduler-name",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid option, invalid scheduler name default",
			args: args{
				logger: testr.New(t),
				config: Configuration{
					EnableBatchScheduler: false,
					BatchScheduler:       "default",
				},
			},
			wantErr: true,
		},
		{
			name: "both enable-batch-scheduler and batch-scheduler are set",
			args: args{
				logger: testr.New(t),
				config: Configuration{
					EnableBatchScheduler: true,
					BatchScheduler:       "volcano",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateBatchSchedulerConfig(tt.args.logger, tt.args.config); (err != nil) != tt.wantErr {
				t.Errorf("ValidateBatchSchedulerConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
