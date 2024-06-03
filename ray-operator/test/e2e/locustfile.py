from locust import FastHttpUser, task, constant, LoadTestShape


class ConstantUser(FastHttpUser):
    wait_time = constant(1)
    network_timeout = None
    connection_timeout = None

    @task
    def hello_world(self):
        self.client.post("/")


# Derived from https://github.com/locustio/locust/blob/master/examples/custom_shape/stages.py
class StagesShape(LoadTestShape):
    stages = [
        {"duration": 60, "users": 10, "spawn_rate": 10},
    ]

    def tick(self):
        run_time = self.get_run_time()
        for stage in self.stages:
            if run_time < stage["duration"]:
                tick_data = (stage["users"], stage["spawn_rate"])
                return tick_data
        return None
