from typing import Any


class Capabilities:
    def __init__(self, add: list[str], drop: list[str]):
        self.add = add
        self.drop = drop

    def to_string(self) -> str:
        return f"add = {self.add}, drop = {self.drop}"

    def to_dict(self) -> dict[str, Any]:
        return {"add": self.add, "drop": self.drop}


class SecurityContext:
    def __init__(self, capabilities: Capabilities = None, privileged: bool = False, runAsUser: int = 500,
                 runAsGroup: int = 100):
        self.capabilities = capabilities
        self.privileged = privileged
        self.runAsUser = runAsUser
        self.runAsGroup = runAsGroup

    def to_string(self) -> str:
        val = f"privileged = {self.privileged}, runAsUser = {self.runAsUser}, runAsGroup = {self.runAsGroup}"
        if self.capabilities is not None:
            val = f"capabilities = {self.capabilities.to_string()}, " + val
        return val

    def to_dict(self) -> dict[str, Any]:
        dct = {"privileged": self.privileged, "runAsUser": self.runAsUser, "runAsGroup": self.runAsGroup}
        if self.capabilities is not None:
            dct["capabilities"] = self.capabilities.to_dict()
        return dct


def security_context_decoder(dct: dict[str, Any]):
    return SecurityContext(
        capabilities=dct.get("capabilities", None),
        privileged=dct.get("privileged", False),
        runAsUser=dct.get("runAsUser", 500),
        runAsGroup=dct.get("runAsGroup", 100),
    )