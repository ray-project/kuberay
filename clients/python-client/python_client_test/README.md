# Overview

## For developers

make sure you have installed setuptool

`pip install -U pip setuptools`

**run the pip command**


from the directory `path/to/kuberay/clients/python-client`

`pip install -e .`

**to uninstall the module run**

`pip uninstall python-client`

## For testing run

`python -m unittest discover 'path/to/kuberay/clients/python-client/python_client_test/'`

### Coverage report

__install prerequisites__

`sudo apt install libsqlite3-dev`

`pip3 install db-sqlite3`

`pyenv install 3.6.5` # or your Python version

`pip install coverage`

__To gather data__
`python -m coverage run -m unittest`

__to generate a coverage report__
`python -m coverage report`

__to generate the test coverage report in HTML format__
`python -m coverage html`
