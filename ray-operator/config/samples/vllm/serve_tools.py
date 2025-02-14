import os
from typing import Dict, Optional, List
import logging

from fastapi import FastAPI
from starlette.requests import Request
from starlette.responses import StreamingResponse, JSONResponse

from ray import serve

from vllm.engine.arg_utils import AsyncEngineArgs
from vllm.engine.async_llm_engine import AsyncLLMEngine
from vllm.entrypoints.openai.cli_args import make_arg_parser
from vllm.entrypoints.openai.protocol import (
    ChatCompletionRequest,
    ChatCompletionResponse,
    ErrorResponse,
)
from vllm.entrypoints.openai.serving_chat import OpenAIServingChat
from vllm.entrypoints.openai.serving_engine import LoRAModulePath, PromptAdapterPath
from vllm.utils import FlexibleArgumentParser
from vllm.entrypoints.logger import RequestLogger

logger = logging.getLogger("ray.serve")

app = FastAPI()

# Define tools as a constant
AVAILABLE_TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "get_current_weather",
            "description": "Get the current weather in a given location",
            "parameters": {
                "type": "object",
                "properties": {
                    "location": {
                        "type": "string",
                        "description": "The city and state, e.g. San Francisco, CA",
                    }
                },
                "required": ["location"],
            }
        }
    }
]

@serve.deployment(name="VLLMDeployment")
@serve.ingress(app)
class VLLMDeployment:
    def __init__(
        self,
        engine_args: AsyncEngineArgs,
        response_role: str,
        lora_modules: Optional[List[LoRAModulePath]] = None,
        prompt_adapters: Optional[List[PromptAdapterPath]] = None,
        request_logger: Optional[RequestLogger] = None,
        chat_template: Optional[str] = None,
    ):
        logger.info(f"Starting with engine args: {engine_args}")
        self.openai_serving_chat = None
        self.engine_args = engine_args
        self.response_role = response_role
        self.lora_modules = lora_modules
        self.prompt_adapters = prompt_adapters
        self.request_logger = request_logger
        self.chat_template = chat_template
        self.engine = AsyncLLMEngine.from_engine_args(engine_args)
        self.tools = AVAILABLE_TOOLS

    @app.post("/v1/chat/completions")
    async def create_chat_completion(
        self, request: Request
    ):
        try:
            # Parse the raw request body
            body = await request.json()
            
            # Convert tools format if present
            if "tools" in body:
                tools_dict = {}
                for tool in body.get("tools", []):
                    if tool["type"] == "function":
                        func = tool["function"]
                        tools_dict[func["name"]] = {
                            "description": func["description"],
                            "parameters": func["parameters"]
                        }
                body["tools"] = tools_dict

            # Create chat request
            chat_request = ChatCompletionRequest(**body)

            if not self.openai_serving_chat:
                model_config = await self.engine.get_model_config()
                # Use the actual model path instead of the HF model ID
                model_name = os.environ.get('MODEL_ID').split('/')[-1]  # Get just the model name part
                self.openai_serving_chat = OpenAIServingChat(
                    self.engine,
                    model_config,
                    served_model_names=[model_name],  # Use simplified model name
                    response_role=self.response_role,
                    chat_template=self.chat_template
                )
            
            # Update the request model to match the served model name
            chat_request.model = os.environ.get('MODEL_ID').split('/')[-1]
            
            logger.info(f"Request: {chat_request}")
            generator = await self.openai_serving_chat.create_chat_completion(
                chat_request, request
            )
            
            if isinstance(generator, ErrorResponse):
                return JSONResponse(
                    content=generator.model_dump(), 
                    status_code=generator.code
                )
            
            if chat_request.stream:
                return StreamingResponse(
                    content=generator, 
                    media_type="text/event-stream"
                )
            else:
                assert isinstance(generator, ChatCompletionResponse)
                return JSONResponse(content=generator.model_dump())
                
        except Exception as e:
            logger.exception(f"Error processing chat completion request: {str(e)}")
            return JSONResponse(
                content={"error": str(e)},
                status_code=500
            )

    async def handle_tool_calls(self, tool_calls: List[Dict]):
        """Handle tool calls from the model"""
        results = []
        for tool_call in tool_calls:
            if tool_call["function"]["name"] == "get_current_weather":
                # Implement your weather API call here
                results.append({
                    "tool_call_id": tool_call["id"],
                    "output": "Weather information would be returned here"
                })
        return results


def parse_vllm_args(cli_args: Dict[str, str]):
    """Parses vLLM args based on CLI inputs.

    Currently uses argparse because vLLM doesn't expose Python models for all of the
    config options we want to support.
    """
    arg_parser = FlexibleArgumentParser(
        description="vLLM OpenAI-Compatible RESTful API server."
    )

    parser = make_arg_parser(arg_parser)
    arg_strings = []
    for key, value in cli_args.items():
        arg_strings.extend([f"--{key}", str(value)])
    logger.info(arg_strings)
    parsed_args = parser.parse_args(args=arg_strings)
    return parsed_args


def build_app(cli_args: Dict[str, str]) -> serve.Application:
    """Builds the Serve app based on CLI arguments.

    See https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html#command-line-arguments-for-the-server
    for the complete set of arguments.

    Supported engine arguments: https://docs.vllm.ai/en/latest/models/engine_args.html.
    """  # noqa: E501
    parsed_args = parse_vllm_args(cli_args)
    engine_args = AsyncEngineArgs.from_cli_args(parsed_args)
    engine_args.worker_use_ray = True

    return VLLMDeployment.bind(
        engine_args,
        parsed_args.response_role,
        parsed_args.lora_modules,
        parsed_args.prompt_adapters,
        cli_args.get("request_logger"),
        parsed_args.chat_template,
    )


model = build_app(
    {
        "model": os.environ['MODEL_ID'], 
        "tensor-parallel-size": os.environ['TENSOR_PARALLELISM'], 
        "pipeline-parallel-size": os.environ['PIPELINE_PARALLELISM'],
        "max-model-len": os.environ['MAX_MODEL_LEN'],
        "gpu-memory-utilization": os.environ['GPU_MEMORY_UTILIZATION'],
     }
    )
