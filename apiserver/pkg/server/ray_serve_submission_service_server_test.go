package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/json"

	//	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	// "google.golang.org/protobuf/types/known/emptypb"
)

const body = `{
	"controller_info": {
	  "node_id": "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
	  "node_ip": "10.244.2.2",
	  "actor_id": "275dd751db38e325a07d921e01000000",
	  "actor_name": "SERVE_CONTROLLER_ACTOR",
	  "worker_id": "8d139efaa21aaebe929a6fde85819df96c8a9a140e50d514c0db590b",
	  "log_file_path": "/serve/controller_20882.log"
	},
	"proxy_location": "EveryNode",
	"http_options": {
	  "host": "0.0.0.0",
	  "port": 8000,
	  "root_path": "",
	  "request_timeout_s": null,
	  "keep_alive_timeout_s": 5
	},
	"grpc_options": {
	  "port": 9000,
	  "grpc_servicer_functions": []
	},
	"proxies": {
	  "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9": {
		"node_id": "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
		"node_ip": "10.244.2.2",
		"actor_id": "04326b7ced4852e076f2629001000000",
		"actor_name": "SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
		"worker_id": "f6acbe0684a784d0f90541c0324aab938d435c49f6c38db18b74cb0b",
		"log_file_path": "/serve/proxy_10.244.2.2.log",
		"status": "HEALTHY"
	  },
	  "e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e": {
		"node_id": "e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e",
		"node_ip": "10.244.1.2",
		"actor_id": "c7a908cab1871a91b8697fc901000000",
		"actor_name": "SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e",
		"worker_id": "e760b7188b5447630d5bd7c37129cc50609c643a79f3f5df96ab9984",
		"log_file_path": "/serve/proxy_10.244.1.2.log",
		"status": "HEALTHY"
	  }
	},
	"deploy_mode": "MULTI_APP",
	"applications": {
	  "fruit_app": {
		"name": "fruit_app",
		"route_prefix": "/fruit",
		"docs_path": null,
		"status": "RUNNING",
		"message": "",
		"last_deployed_time_s": 1701951615.140295,
		"deployed_app_config": {
		  "name": "fruit_app",
		  "route_prefix": "/fruit",
		  "import_path": "fruit.deployment_graph",
		  "runtime_env": {
			"working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
		  },
		  "deployments": [
			{
			  "name": "MangoStand",
			  "num_replicas": 1,
			  "user_config": {
				"price": 3
			  },
			  "ray_actor_options": {
				"num_cpus": 0.1
			  }
			},
			{
			  "name": "OrangeStand",
			  "num_replicas": 1,
			  "user_config": {
				"price": 2
			  },
			  "ray_actor_options": {
				"num_cpus": 0.1
			  }
			},
			{
			  "name": "PearStand",
			  "num_replicas": 1,
			  "user_config": {
				"price": 1
			  },
			  "ray_actor_options": {
				"num_cpus": 0.1
			  }
			},
			{
			  "name": "FruitMarket",
			  "num_replicas": 1,
			  "ray_actor_options": {
				"num_cpus": 0.1
			  }
			},
			{
			  "name": "DAGDriver",
			  "num_replicas": 1,
			  "ray_actor_options": {
				"num_cpus": 0.1
			  }
			}
		  ]
		},
		"deployments": {
		  "MangoStand": {
			"name": "MangoStand",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "MangoStand",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": {
				"price": 3
			  },
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
				"node_ip": "10.244.2.2",
				"actor_id": "36dd428b157f61712560ccb201000000",
				"actor_name": "SERVE_REPLICA::fruit_app#MangoStand#gCmCHD",
				"worker_id": "e08d413ffd1c1c3543f09b928e85a6fad6e9f58dacd5667e7e84b903",
				"log_file_path": "/serve/deployment_MangoStand_fruit_app#MangoStand#gCmCHD.log",
				"replica_id": "fruit_app#MangoStand#gCmCHD",
				"state": "RUNNING",
				"pid": 21043,
				"start_time_s": 1701951617.602063
			  }
			]
		  },
		  "OrangeStand": {
			"name": "OrangeStand",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "OrangeStand",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": {
				"price": 2
			  },
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e",
				"node_ip": "10.244.1.2",
				"actor_id": "cd4cc408d1510314a5a5f2ba01000000",
				"actor_name": "SERVE_REPLICA::fruit_app#OrangeStand#hXnOxP",
				"worker_id": "77427ce31ac509e2ea9d878a323b1e58fa88d87660323a92a4b9a28c",
				"log_file_path": "/serve/deployment_OrangeStand_fruit_app#OrangeStand#hXnOxP.log",
				"replica_id": "fruit_app#OrangeStand#hXnOxP",
				"state": "RUNNING",
				"pid": 20542,
				"start_time_s": 1701951617.6038337
			  }
			]
		  },
		  "PearStand": {
			"name": "PearStand",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "PearStand",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": {
				"price": 1
			  },
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
				"node_ip": "10.244.2.2",
				"actor_id": "45e3c1bb13c2e6ce8359c55301000000",
				"actor_name": "SERVE_REPLICA::fruit_app#PearStand#tnBMZm",
				"worker_id": "e15cbf5147fdeccbc664fa581d48c0393e6ead2105eb2940610fc73f",
				"log_file_path": "/serve/deployment_PearStand_fruit_app#PearStand#tnBMZm.log",
				"replica_id": "fruit_app#PearStand#tnBMZm",
				"state": "RUNNING",
				"pid": 21044,
				"start_time_s": 1701951617.6056767
			  }
			]
		  },
		  "FruitMarket": {
			"name": "FruitMarket",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "FruitMarket",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": null,
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e",
				"node_ip": "10.244.1.2",
				"actor_id": "1b8728dca6dbc1afdd0ba5fb01000000",
				"actor_name": "SERVE_REPLICA::fruit_app#FruitMarket#ZkThVO",
				"worker_id": "60b0c1d77d291e01843e1fd9b8b84dea55e1a76caf906bcbe559bb4b",
				"log_file_path": "/serve/deployment_FruitMarket_fruit_app#FruitMarket#ZkThVO.log",
				"replica_id": "fruit_app#FruitMarket#ZkThVO",
				"state": "RUNNING",
				"pid": 20543,
				"start_time_s": 1701951617.6073825
			  }
			]
		  },
		  "DAGDriver": {
			"name": "DAGDriver",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "DAGDriver",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": null,
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
				"node_ip": "10.244.2.2",
				"actor_id": "1cf4c3d57b8bad81f4ab6bcb01000000",
				"actor_name": "SERVE_REPLICA::fruit_app#DAGDriver#kLPIQf",
				"worker_id": "f0b4328c28f7e69c70d79d298b7677c2a8c81a26327961805692ebdf",
				"log_file_path": "/serve/deployment_DAGDriver_fruit_app#DAGDriver#kLPIQf.log",
				"replica_id": "fruit_app#DAGDriver#kLPIQf",
				"state": "RUNNING",
				"pid": 21069,
				"start_time_s": 1701951617.6094174
			  }
			]
		  }
		}
	  },
	  "math_app": {
		"name": "math_app",
		"route_prefix": "/calc",
		"docs_path": null,
		"status": "RUNNING",
		"message": "",
		"last_deployed_time_s": 1701951615.140295,
		"deployed_app_config": {
		  "name": "math_app",
		  "route_prefix": "/calc",
		  "import_path": "conditional_dag.serve_dag",
		  "runtime_env": {
			"working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
		  },
		  "deployments": [
			{
			  "name": "Adder",
			  "num_replicas": 1,
			  "user_config": {
				"increment": 3
			  },
			  "ray_actor_options": {
				"num_cpus": 0.1
			  }
			},
			{
			  "name": "Multiplier",
			  "num_replicas": 1,
			  "user_config": {
				"factor": 5
			  },
			  "ray_actor_options": {
				"num_cpus": 0.1
			  }
			},
			{
			  "name": "Router",
			  "num_replicas": 1
			},
			{
			  "name": "create_order",
			  "num_replicas": 1
			},
			{
			  "name": "DAGDriver",
			  "num_replicas": 1
			}
		  ]
		},
		"deployments": {
		  "Multiplier": {
			"name": "Multiplier",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "Multiplier",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": {
				"factor": 5
			  },
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e",
				"node_ip": "10.244.1.2",
				"actor_id": "041f6038590609710c30e27301000000",
				"actor_name": "SERVE_REPLICA::math_app#Multiplier#vMKRYG",
				"worker_id": "cf08a296fca95b4737c79ba48ca9c4f91d7a1a1bb5bede3bf7731315",
				"log_file_path": "/serve/deployment_Multiplier_math_app#Multiplier#vMKRYG.log",
				"replica_id": "math_app#Multiplier#vMKRYG",
				"state": "RUNNING",
				"pid": 20592,
				"start_time_s": 1701951618.1508813
			  }
			]
		  },
		  "Adder": {
			"name": "Adder",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "Adder",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": {
				"increment": 3
			  },
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
				"node_ip": "10.244.2.2",
				"actor_id": "8471cd644fbadea6bac4743c01000000",
				"actor_name": "SERVE_REPLICA::math_app#Adder#PwiijJ",
				"worker_id": "e4ec7d3348ca45b62a76810c701d13d6eb3ada11d6186bfccfd7a234",
				"log_file_path": "/serve/deployment_Adder_math_app#Adder#PwiijJ.log",
				"replica_id": "math_app#Adder#PwiijJ",
				"state": "RUNNING",
				"pid": 21095,
				"start_time_s": 1701951618.1523902
			  }
			]
		  },
		  "Router": {
			"name": "Router",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "Router",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": null,
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e",
				"node_ip": "10.244.1.2",
				"actor_id": "656f45d5be4142408daec8b501000000",
				"actor_name": "SERVE_REPLICA::math_app#Router#TeUoln",
				"worker_id": "ae5e11848cb8c0f2afe4af8f24a3589b2b280de55b46a45fcf5bbaef",
				"log_file_path": "/serve/deployment_Router_math_app#Router#TeUoln.log",
				"replica_id": "math_app#Router#TeUoln",
				"state": "RUNNING",
				"pid": 20593,
				"start_time_s": 1701951618.1552744
			  }
			]
		  },
		  "create_order": {
			"name": "create_order",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "create_order",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": null,
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 0.1
			  }
			},
			"replicas": [
			  {
				"node_id": "636c9c875a2c71626fbff90dd39ef49c20cf15e6639ba45ba09065f9",
				"node_ip": "10.244.2.2",
				"actor_id": "eaec4b6168527f465f0578dc01000000",
				"actor_name": "SERVE_REPLICA::math_app#create_order#GXxUsY",
				"worker_id": "e933bbfe8f719897f8ca128d9115c10d92f968835a6761b724f6419a",
				"log_file_path": "/serve/deployment_create_order_math_app#create_order#GXxUsY.log",
				"replica_id": "math_app#create_order#GXxUsY",
				"state": "RUNNING",
				"pid": 21147,
				"start_time_s": 1701951618.1870356
			  }
			]
		  },
		  "DAGDriver": {
			"name": "DAGDriver",
			"status": "HEALTHY",
			"message": "",
			"deployment_config": {
			  "name": "DAGDriver",
			  "num_replicas": 1,
			  "max_concurrent_queries": 100,
			  "user_config": null,
			  "graceful_shutdown_wait_loop_s": 2,
			  "graceful_shutdown_timeout_s": 20,
			  "health_check_period_s": 10,
			  "health_check_timeout_s": 30,
			  "ray_actor_options": {
				"runtime_env": {
				  "working_dir": "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip",
				  "env_vars": {}
				},
				"num_cpus": 1
			  }
			},
			"replicas": [
			  {
				"node_id": "e066bf5cf738453add1999241965ef7facb9a7eaa7e03a49b91f462e",
				"node_ip": "10.244.1.2",
				"actor_id": "cfbae868979d619045798def01000000",
				"actor_name": "SERVE_REPLICA::math_app#DAGDriver#lAlMJu",
				"worker_id": "6c5b550e9ecfac4ace42151c96fe3fd1bbf50dd809af5a78c48b9758",
				"log_file_path": "/serve/deployment_DAGDriver_math_app#DAGDriver#lAlMJu.log",
				"replica_id": "math_app#DAGDriver#lAlMJu",
				"state": "RUNNING",
				"pid": 20676,
				"start_time_s": 1701951618.189014
			  }
			]
		  }
		}
	  }
	}
  }`

func TestMarshallingServeData(t *testing.T) {
	var serveDetails utils.ServeDetails
	err := json.Unmarshal([]byte(body), &serveDetails)
	assert.Equal(t, err, nil)

	serveDetailsPB := convertServeDetails(&serveDetails)
	assert.Equal(t, serveDetailsPB.DeployMode, "MULTI_APP")
	assert.Equal(t, serveDetailsPB.ProxyLocation, "EveryNode")
	assert.Equal(t, len(serveDetailsPB.Proxies), 2)
	assert.Equal(t, len(serveDetailsPB.Applications), 2)
	for _, app := range serveDetailsPB.Applications {
		switch app.Name {
		case "fruit_app":
			assert.Equal(t, len(app.Deployments), 5)
			assert.Equal(t, len(app.DeployedAppConfig.Deployments), 5)
		case "math_app":
			assert.Equal(t, len(app.Deployments), 5)
			assert.Equal(t, len(app.DeployedAppConfig.Deployments), 5)
		default:
			t.Error("unexpected application name ", app.Name)
		}
	}
}
