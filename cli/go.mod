module github.com/ray-project/kuberay/cli

go 1.20

require (
	github.com/fatih/color v1.13.0
	github.com/kris-nova/logger v0.2.2
	github.com/kris-nova/lolgopher v0.0.0-20210112022122-73f0047e8b65
	github.com/olekukonko/tablewriter v0.0.5
	github.com/ray-project/kuberay/proto v0.0.0-20220119062608-4054f1bf1765
	github.com/spf13/cobra v1.3.0
	github.com/spf13/viper v1.10.1
	google.golang.org/grpc v1.59.0
	k8s.io/klog/v2 v2.20.0
)

require (
	github.com/fsnotify/fsnotify v1.5.1 // indirect
	github.com/go-logr/logr v1.0.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.6.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/magiconair/properties v1.8.5 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mitchellh/mapstructure v1.4.3 // indirect
	github.com/pelletier/go-toml v1.9.4 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/spf13/afero v1.6.0 // indirect
	github.com/spf13/cast v1.4.1 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/subosito/gotenv v1.2.0 // indirect
	golang.org/x/net v0.20.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto v0.0.0-20230822172742-b8732ec3820d // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20230822172742-b8732ec3820d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230822172742-b8732ec3820d // indirect
	google.golang.org/protobuf v1.32.0 // indirect
	gopkg.in/ini.v1 v1.66.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace github.com/ray-project/kuberay/proto => ../proto
