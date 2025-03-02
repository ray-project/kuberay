from ray import serve

@serve.deployment(
    ray_actor_options={
        "num_cpus": 0.1,
    }
)
class BaseService:
    async def __call__(self):
        return "hello world"

DagNode = BaseService.bind()
