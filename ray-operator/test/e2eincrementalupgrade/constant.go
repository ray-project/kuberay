package e2eincrementalupgrade

type serveConfigV2 string

// defaultIncrementalUpgradeServeConfigV2 configures a Serve app for basic incremental upgrade tests.
// const defaultIncrementalUpgradeServeConfigV2 serveConfigV2 = `applications:
//   - name: fruit_app
//     import_path: fruit.deployment_graph
//     route_prefix: /fruit
//     runtime_env:
//       working_dir: "https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
//     deployments:
//       - name: MangoStand
//         num_replicas: 1
//         user_config:
//           price: 3
//         ray_actor_options:
//           num_cpus: 0.1
//       - name: OrangeStand
//         num_replicas: 1
//         user_config:
//           price: 2
//         ray_actor_options:
//           num_cpus: 0.1
//       - name: FruitMarket
//         num_replicas: 1
//         ray_actor_options:
//           num_cpus: 0.1
//   - name: math_app
//     import_path: conditional_dag.serve_dag
//     route_prefix: /calc
//     runtime_env:
//       working_dir: "https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
//     deployments:
//       - name: Adder
//         num_replicas: 1
//         user_config:
//           increment: 3
//         ray_actor_options:
//           num_cpus: 0.1
//       - name: Multiplier
//         num_replicas: 1
//         user_config:
//           factor: 5
//         ray_actor_options:
//           num_cpus: 0.1
//       - name: Router
//         num_replicas: 1
//         ray_actor_options:
//           num_cpus: 0.1
// `

// highRPSServeConfigV2 configures a minimal high-RPS Serve app (SimpleDeployment) for Locust load tests.
const highRPSServeConfigV2 serveConfigV2 = `applications:
  - name: simple_app
    import_path: simple_serve.app
    route_prefix: /test
    runtime_env:
      working_dir: "https://github.com/jiangjiawei1103/incr-upgrade-locust/archive/main.zip"
    deployments:
      - name: SimpleDeployment
        autoscaling_config:
          min_replicas: 1
          max_replicas: 3
          target_ongoing_requests: 2
          max_ongoing_requests: 6
          upscale_delay_s: 0.5
        ray_actor_options:
          num_cpus: 2
`
