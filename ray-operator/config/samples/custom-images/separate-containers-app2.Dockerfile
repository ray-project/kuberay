FROM rayproject/ray:nightly-py38-cpu

RUN echo -e '\
from ray import serve\n\
@serve.deployment\n\
class MyModel:\n\
    def __call__(self):\n\
        with open("file.txt") as f:\n\
            return f.read()\n\
        return content\n\
app = MyModel.bind()'\
>> /home/ray/serve_application.py

RUN echo "Good morning Bob!" > /home/ray/file.txt
