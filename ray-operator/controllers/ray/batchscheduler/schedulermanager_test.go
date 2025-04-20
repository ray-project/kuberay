package batchscheduler

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/volcano"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/yunikorn"
)

func TestGetSchedulerFactory(t *testing.T) {
	DefaultFactory := &schedulerinterface.DefaultBatchSchedulerFactory{}
	VolcanoFactory := &volcano.VolcanoBatchSchedulerFactory{}
	YuniKornFactory := &yunikorn.YuniKornSchedulerFactory{}

	type args struct {
		rayConfigs v1alpha1.Configuration
	}
	tests := []struct {
		want           reflect.Type
		name           string
		expectedErrMsg string
		args           args
	}{
		{
			name: "enableBatchScheduler=false, batchScheduler=''",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					EnableBatchScheduler: false,
					BatchScheduler:       "",
				},
			},
			want: reflect.TypeOf(DefaultFactory),
		},
		{
			name: "enableBatchScheduler=false, batchScheduler not set",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					EnableBatchScheduler: false,
				},
			},
			want: reflect.TypeOf(DefaultFactory),
		},
		{
			name: "enableBatchScheduler=false, batchScheduler set to yunikorn",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					EnableBatchScheduler: false,
					BatchScheduler:       yunikorn.GetPluginName(),
				},
			},
			want: reflect.TypeOf(YuniKornFactory),
		},
		{
			name: "enableBatchScheduler=false, batchScheduler set to volcano",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					EnableBatchScheduler: false,
					BatchScheduler:       volcano.GetPluginName(),
				},
			},
			want: reflect.TypeOf(VolcanoFactory),
		},
		{
			name: "enableBatchScheduler not set, batchScheduler set to yunikorn",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					BatchScheduler: yunikorn.GetPluginName(),
				},
			},
			want: reflect.TypeOf(YuniKornFactory),
		},
		{
			name: "enableBatchScheduler not set, batchScheduler set to volcano",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					BatchScheduler: volcano.GetPluginName(),
				},
			},
			want: reflect.TypeOf(VolcanoFactory),
		},
		{
			name: "enableBatchScheduler not set, batchScheduler set to unknown value",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					BatchScheduler: "unknown-scheduler-name",
				},
			},
			expectedErrMsg: "the scheduler is not supported, name=unknown-scheduler-name",
		},
		{
			// for backwards compatibility, if enableBatchScheduler=true, always use volcano
			name: "enableBatchScheduler=true, batchScheduler set to yunikorn",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					EnableBatchScheduler: true,
					BatchScheduler:       yunikorn.GetPluginName(),
				},
			},
			want: reflect.TypeOf(VolcanoFactory),
		},
		{
			// for backwards compatibility, if enableBatchScheduler=true, always use volcano
			name: "enableBatchScheduler=true, batchScheduler set to volcano",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					EnableBatchScheduler: true,
					BatchScheduler:       volcano.GetPluginName(),
				},
			},
			want: reflect.TypeOf(VolcanoFactory),
		},
		{
			// for backwards compatibility, if enableBatchScheduler=true, always use volcano
			name: "enableBatchScheduler=true, batchScheduler set to volcano",
			args: args{
				rayConfigs: v1alpha1.Configuration{
					EnableBatchScheduler: true,
					BatchScheduler:       "",
				},
			},
			want: reflect.TypeOf(VolcanoFactory),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getSchedulerFactory(tt.args.rayConfigs)
			if len(tt.expectedErrMsg) > 0 {
				require.Errorf(t, err, tt.expectedErrMsg)
				return
			}

			if reflect.TypeOf(got) != tt.want {
				t.Errorf("getSchedulerFactory() = %v, want %v", got, tt.want)
			}
		})
	}
}
