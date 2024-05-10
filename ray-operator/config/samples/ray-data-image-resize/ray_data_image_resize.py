from typing import Dict

import numpy as np
import ray
from torchvision import transforms

DATA_URI = "gs://anonymous@ray-images/images"

def main():
    """
    This is a CPU-only job that reads images from
    Google Cloud Storage and resizes them.
    """
    ray.init()
    dataset = ray.data.read_images(DATA_URI, include_paths=True)
    dataset = dataset.map(transform_frame)
    dataset = dataset.select_columns(["path"])
    dataset_iter = dataset.iter_batches(batch_size=None)
    for _ in dataset_iter:
        pass

def transform_frame(row: Dict[str, np.ndarray]) -> Dict[str, np.ndarray]:
    transform = transforms.Compose(
        [transforms.ToTensor(), transforms.Resize((256, 256))]
    )
    row["image"] = transform(row["image"])
    return row


if __name__ == "__main__":
    main()
