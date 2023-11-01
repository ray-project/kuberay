import enum


class HostPath(enum.Enum):
    DIRECTORY = 0
    FILE = 1


class MountPropagationMode(enum.Enum):
    NONE = 0
    HOSTTOCONTAINER = 1
    BIDIRECTIONAL = 2


class AccessMode(enum.Enum):
    RWO = 0
    ROX = 1
    RWX = 2


class BaseVolume:
    """
     KubeRay currently support several types of volumes, including hostPat, PVC,
     ephemeral volumes, config maps, secrets and empty dir. All of them use slightly
     different parameters. Base Volume is a base class for all different volume types.
    """
    def to_string(self) -> str:
        raise Exception(f"Base volume cannot be used directly. Pls use one of the derived classes")

    def to_dict(self) -> dict[str, any]:
        raise Exception(f"Base volume cannot be used directly. Pls use one of the derived classes")


class HostPathVolume(BaseVolume):
    """
        This class implements HostPath volume. In addition to name and mount path it requires host
        path volume specific parameters:
            source - data location on host
            hostPathType - host path type: directory (0) or file (1)
            mountPropagationMode - mount propagation: None (0), host to container (1) or bidirectional (2)

    """
    def __init__(self, name: str, mount_path: str, source: str, hostpathtype: HostPath = None,
                 mountpropagation: MountPropagationMode = None) -> None:
        self.name = name
        self.mount_path = mount_path
        self.source = source
        self.hostpathtype = hostpathtype
        self.volume_type = 1
        self.mountpropagation = mountpropagation

    def to_string(self) -> str:
        val = (f"name = {self.name}, mount_path = {self.mount_path}, source = {self.source}, "
               f"volume type = hostPath")
        if self.mountpropagation is not None:
            val += f", mount propagation = {self.mountpropagation.name}"
        if self.hostpathtype is not None:
            val += f", host path type = {self.hostpathtype.name}"
        return val

    def to_dict(self) -> dict[str, any]:
        dst = {"name": self.name, "mountPath": self.mount_path, "source": self.source,
               "volumeType": self.volume_type}
        if self.mountpropagation is not None:
            dst["mountPropagationMode"] = self.mountpropagation.value
        if self.hostpathtype is not None:
            dst["hostPathType"] = self.hostpathtype.value
        return dst


class PVCVolume(BaseVolume):
    """
        This class implements PVC volume. In addition to name and mount path it requires
        PVC volume specific parameters:
           source - PVC claim name
           read_only - read only flag
           mountPropagationMode - mount propagation: None (0), host to container (1) or bidirectional (2)
    """
    def __init__(self, name: str, mount_path: str, source: str, read_only: bool = False,
                 mountpropagation: MountPropagationMode = None) -> None:
        self.name = name
        self.mount_path = mount_path
        self.source = source
        self.volume_type = 0
        self.mountpropagation = mountpropagation
        self.readonly = read_only

    def to_string(self) -> str:
        val = (f"name = {self.name}, mount_path = {self.mount_path}, source = {self.source}, "
               f"volume type = PVC")
        if self.readonly:
            val += ", read only = True"
        if self.mountpropagation is not None:
            val += f", mount propagation = {self.mountpropagation.name}"
        return val

    def to_dict(self) -> dict[str, any]:
        dst = {"name": self.name, "mountPath": self.mount_path, "source": self.source,
               "volumeType": self.volume_type}
        if self.readonly:
            dst["readOnly"] = True
        if self.mountpropagation is not None:
            dst["mountPropagationMode"] = self.mountpropagation.value
        return dst


class EphemeralVolume(BaseVolume):
    """
        This class implements Ephemeral volume. In addition to name and mount path it requires
        Ephemeral volume specific parameters:
            storage - disk size (valid k8 value, for example 5Gi)
            storageClass - storage class - optional, if not specified, use default
            accessMode - access mode RWO - optional ReadWriteOnce (0), ReadOnlyMany (1), ReadWriteMany (2)
            mountPropagationMode - optional mount propagation: None (0), host to container (1) or bidirectional (2)
    """
    def __init__(self, name: str, mount_path: str, storage: str, storage_class: str = None,
                 accessmode: AccessMode = None,  mountpropagation: MountPropagationMode = None) -> None:
        self.name = name
        self.mount_path = mount_path
        self.storage = storage
        self.volume_type = 2
        self.mountpropagation = mountpropagation
        self.storageclass = storage_class
        self.accessmode = accessmode

    def to_string(self) -> str:
        val = (f"name = {self.name}, mount_path = {self.mount_path}, storage = {self.storage} "
               f"volume type = ephemeral")
        if self.storageclass is not None:
            val += f", storage class = {self.storageclass}"
        if self.accessmode is not None:
            val += f", access mode = {self.accessmode.name}"
        if self.mountpropagation is not None:
            val += f", mount propagation = {self.mountpropagation.name}"
        return val

    def to_dict(self) -> dict[str, any]:
        dct = {"name": self.name, "mountPath": self.mount_path, "storage": self.storage,
               "volumeType": self.volume_type}
        if self.storageclass is not None:
            dct["storageClassName"] = self.storageclass
        if self.accessmode is not None:
            dct["accessMode"] = self.accessmode.value
        if self.mountpropagation is not None:
            dct["mountPropagationMode"] = self.mountpropagation.value
        return dct


class EmptyDirVolume(BaseVolume):
    """
        This class implements EmptyDir volume. In addition to name and mount path it requires
        Empty Dir specific parameters:
            storage - optional max storage size (valid k8 value, for example 5Gi)
    """
    def __init__(self, name: str, mount_path: str, storage: str = None) -> None:
        self.name = name
        self.mount_path = mount_path
        self.storage = storage
        self.volume_type = 5

    def to_string(self) -> str:
        val = f"name = {self.name}, mount_path = {self.mount_path}, volume type = emptyDir"
        if self.storage is not None:
            val += f", storage = {self.storage}"
        return val

    def to_dict(self) -> dict[str, any]:
        dct = {"name": self.name, "mountPath": self.mount_path, "volumeType": self.volume_type}
        if self.storage is not None:
            dct["storage"] = self.storage
        return dct


class ConfigMapVolume(BaseVolume):
    """
        This class implements ConfigMap volume. In addition to name and mount path it requires
        configMap volume specific parameters:
            source - required, config map name
            items - optional, key/path items (optional)
    """
    def __init__(self, name: str, mount_path: str, source: str, items: dict[str, str] = None,) -> None:
        self.name = name
        self.mount_path = mount_path
        self.source = source
        self.items = items
        self.volume_type = 3

    def to_string(self) -> str:
        val = (f"name = {self.name}, mount_path = {self.mount_path}, source = {self.source}, "
               f"volume type = configmap")
        if self.items is not None:
            val = val + f", itemss = {str(self.items)}"
        return val

    def to_dict(self) -> dict[str, any]:
        dct = {"name": self.name, "mountPath": self.mount_path, "source": self.source,
               "volumeType": self.volume_type}
        if self.items is not None:
            dct["items"] = self.items
        return dct


class SecretVolume(BaseVolume):
    """
        This class implements Secret volume. In addition to name and mount path it requires
        Secret volume specific parameters:
            source - required, secret name
            items - optional, key/path items (optional)
    """
    def __init__(self, name: str, mount_path: str, source: str, items: dict[str, str] = None,) -> None:
        self.name = name
        self.mount_path = mount_path
        self.source = source
        self.items = items
        self.volume_type = 4

    def to_string(self) -> str:
        val = (f"name = {self.name}, mount_path = {self.mount_path}, source = {self.source}, "
               f"volume type = secret")
        if self.items is not None:
            val = val + f", itemss = {str(self.items)}"
        return val

    def to_dict(self) -> dict[str, any]:
        dct = {"name": self.name, "mountPath": self.mount_path, "source": self.source,
               "volumeType": self.volume_type}
        if self.items is not None:
            dct["items"] = self.items
        return dct


"""
    Creates new Volume from dictionary, used for unmarshalling json. Python does not
    support multiple constructors, so do it this way
"""


def volume_decoder(dst: dict[str, any]) -> BaseVolume:

    def _getmountpropagatio() -> MountPropagationMode:
        if "mountPropagationMode" in dst:
            return MountPropagationMode(int(dst.get("mountPropagationMode", "0")))
        return None

    def _gethostpathtype() -> HostPath:
        if "hostPathType" in dst:
            return HostPath(int(dst.get("hostPathType", "0")))
        return None

    def _getaccessmode() -> AccessMode:
        if "accessMode" in dst:
            return AccessMode(int(dst.get("accessMode", "0")))
        return None

    match dst["volumeType"]:
        case 0:
            # PVC
            return PVCVolume(name=dst.get("name", ""), mount_path=dst.get("mountPath", ""),
                             source=dst.get("source", ""), read_only=dst.get("readOnly", False),
                             mountpropagation=_getmountpropagatio())
        case 1:
            # hostpath
            return HostPathVolume(name=dst.get("name", ""), mount_path=dst.get("mountPath", ""),
                                  source=dst.get("source", ""), hostpathtype=_gethostpathtype(),
                                  mountpropagation=_getmountpropagatio())
        case 2:
            # Ephemeral volume
            return EphemeralVolume(name=dst.get("name", ""), mount_path=dst.get("mountPath", ""),
                                   storage=dst.get("storage", ""), storage_class=dst.get("storageClassName"),
                                   accessmode=_getaccessmode(), mountpropagation=_getmountpropagatio())
        case 3:
            # ConfigMap Volume
            return ConfigMapVolume(name=dst.get("name", ""), mount_path=dst.get("mountPath", ""),
                                   source=dst.get("source", ""), items=dst.get("items"))
        case 4:
            # Secret Volume
            return SecretVolume(name=dst.get("name", ""), mount_path=dst.get("mountPath", ""),
                                source=dst.get("source", ""), items=dst.get("items"))
        case 5:
            # Empty dir volume
            return EmptyDirVolume(name=dst.get("name", ""), mount_path=dst.get("mountPath", ""),
                                  storage=dst.get("storage"))
        case default:
            raise Exception(f"Unknown volume type in {dst}")
