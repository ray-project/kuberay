import enum


class EnvarSource(enum.Enum):
    CONFIGMAP = 0
    SECRET = 1
    RESOURCEFIELD = 2
    FIELD = 3


class EnvVarFrom:
    """
    EnvVarFrom is used to define an environment variable from one of the sorces (EnvarSource).
    It provides APIs to create, stringify, convert to dict and json.

    Methods:
    - Create env variable from: gets the following parameters:
        Source required - source of environment variable
        name required name for config map or secret, container name for resource, path for field
        key required Key for config map or secret, resource name for resource
    - to_string() -> str: convert toleration to string for printing
    - to_dict() -> dict[str, any] convert to dict
    """
    def __init__(self, source: EnvarSource, name: str, key: str) -> None:
        self.source = source
        self.name = name
        self.key = key

    def to_string(self) -> str:
        return f"source = {self.source.name}, name = {self.name}, key = {self.key}"

    def to_dict(self) -> dict[str, any]:
        return {"source": self.source.value, "name": self.name, "key": self.key}


class EnvironmentVariables:
    """
    EnvironmentVariables is used to define environment variables.
    It provides APIs to create, stringify, convert to dict and json.

    Methods:
    - Create env variable from: gets the following parameters:
        keyvalue - optional, dictionary of key/value environment variables
        fromref - optional, dictionary of reference environment variables
    - to_string() -> str: convert toleration to string for printing
    - to_dict() -> dict[str, any] convert to dict
    """
    def __init__(self, keyvalue: dict[str, str] = None, fromref: dict[str, EnvVarFrom] = None) -> None:
        self.keyval = keyvalue
        self.fromref = fromref

    def to_string(self) -> str:
        val = ""
        if self.keyval is not None:
            val = f"values = {str(self.keyval)}"
        if self.fromref is not None:
            if val != "":
                val += " , "
            val += "valuesFrom = {"
            first = True
            for k, v in self.fromref.items():
                if not first:
                    val += ", "
                else:
                    first = False
                val += f"{k} = [{v.to_string()}]"
            val += "}"
        return val

    def to_dict(self) -> dict[str, any]:
        dst = {}
        if self.keyval is not None:
            dst["values"] = self.keyval
        if self.fromref is not None:
            fr = {}
            for k, v in self.fromref.items():
                fr[k] = v.to_dict()
            dst["valuesFrom"] = fr
        return dst


"""
    Creates new environment variable from from dictionary, used for unmarshalling json. Python does not
    support multiple constructors, so do it this way
"""


def envvarfrom_decoder(dct: dict[str, any]) -> EnvVarFrom:
    return EnvVarFrom(name=dct.get("name", ""), source=EnvarSource(int(dct.get("source", 0))), key=dct.get("key", ""))


def environmentvariables_decoder(dst: dict[str, any]) -> EnvironmentVariables:
    keyvalues = None
    fr = None
    if "values" in dst:
        keyvalues = dst.get("values")
    if "valuesFrom" in dst:
        fromref = dst.get("valuesFrom")
        fr = {}
        for k, v in fromref.items():
            fr[k] = envvarfrom_decoder(v)
    return EnvironmentVariables(keyvalue=keyvalues, fromref=fr)
