"""Test the kuberay/tests/framework/utils functions."""
import os
import logging
import unittest
import functools


from utils import (
    CONST,
    retrieve_images
)

logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO
)

class FrameworkUtilsTestCase(unittest.TestCase):
    """Test the kuberay/tests/framework/utils functions."""

    def test_retrieve_images(self):
        """
        retrieve all the images from yaml files under ray-operator/config/samples folder
        """
        test_dict ={
            'ray-cluster.autoscaler.yaml'       : {'busybox:1.28', 'rayproject/ray:2.2.0'},
            'ray-cluster.autoscaler.large.yaml' : {'busybox:1.28', 'rayproject/ray:2.2.0'},
            'ray-cluster.ingress.yaml'          : {'rayproject/ray:2.2.0'},
            'ray-cluster.separate-ingress.yaml' : {'rayproject/ray:2.2.0'},
            'ray-cluster.getting-started.yaml'  : {'rayproject/ray:2.2.0'},
            'ray_v1alpha1_rayjob.yaml'          : {'busybox:1.28', 'rayproject/ray:2.2.0'},
            'ray-cluster.external-redis.yaml'   : {'redis:5.0.8', 'busybox:1.28',
                                                   'rayproject/ray:2.2.0'},
            'ray-cluster.heterogeneous.yaml'    : {'busybox:1.28', 'rayproject/ray:2.2.0'},
            'ray-cluster.complete.yaml'         : {'busybox:1.28', 'rayproject/ray:2.2.0'},
            'ray-cluster.complete.large.yaml'   : {'busybox:1.28', 'rayproject/ray:2.2.0'},
            'ray-cluster.mini.yaml'             : {'rayproject/ray:2.2.0'},
            'ray_v1alpha1_rayservice.yaml'      : {'busybox:1.28', 'rayproject/ray:2.2.0'},
            'alb-ingress.yaml'                  : set()
        }
        paths = []
        sample_yaml_dir = CONST.REPO_ROOT.joinpath("ray-operator/config/samples")
        #test for individual file
        for (dirpath,_, filenames) in os.walk(sample_yaml_dir):
            for filename in filenames:
                path = os.path.join(dirpath, filename)
                paths.append(path)
                self.assertTrue(
                    filename in test_dict,
                    msg = f'PLease add {filename}\'s images info in test_framework_utils.py'
                )
                logger.info('[test] retrieve all images from  %s',filename)
                image_dict = retrieve_images([path])
                self.assertTrue(
                    test_dict[filename] == set(image_dict.values()),
                    msg = (f'retrieved images from {filename} fails. '
                           'Condiser update image info in test_framework_utils.py'
                    )
                )
        #test for bunch of files
        logger.info('[test] retrieve all images under folder: %s',sample_yaml_dir)
        image_dict = retrieve_images(paths)
        self.assertTrue(
        functools.reduce(lambda x,y: x.union(y),test_dict.values()) == set(image_dict.values()),
        msg = (f'retrieve images for all files under {sample_yaml_dir} fails. '
               'Condiser update image info in test_framework_utils.py')
        )

if __name__ == '__main__':
    unittest.main(verbosity=2)
