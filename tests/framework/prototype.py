import yaml
import copy
from typing import Optional
from kubernetes import client, config
import os
import time
import docker

class DeltaSet:
    def __init__(self, path, candidates):
        self.path = path
        self.candidates = candidates

    def iterate(self):
        for candidate in self.candidates:
            yield candidate

    # return value: (1) steps (2) None => this path does not exist
    def find_path(self, cr) -> Optional[list[str]]:
        steps = self.path.split('.')
        return steps

def search_path(cr, steps):
    curr = cr
    for step in steps:
        if step.isnumeric():
            int_step = int(step)
            if int_step >= len(curr) or int_step < 0:
                return None
            curr = curr[int(step)]
        elif step in curr:
            curr = curr[step]
        else:
            return None
    return curr

class Mutator:
    def __init__(self, baseCR, deltaSets: list[DeltaSet]):
        self.baseCR = baseCR
        self.deltaSets = deltaSets
    def mutate(self):
        pass
    def apply_delta(self, base, delta, steps):
        root = copy.deepcopy(base)
        curr = search_path(root, steps[:-1])
        curr[steps[-1]] = delta
        return root

class Rule:
    def __init__(self):
        pass
    def trigger_condition(self, cr) -> bool:
        pass
    def assertRule(self, cr):
        pass

class RuleSet:
    def __init__(self, rules: list[Rule]):
        self.rules = rules
    def checkRuleSet(self, cr, namespace):
        for rule in self.rules:
            if rule.trigger_condition(cr):
                rule.assertRule(cr, namespace)

class CREvent:
    def __init__(self, cr, cmd, ruleSets: list[RuleSet], timeout, namespace):
        self.ruleSets = ruleSets
        self.cmd = cmd
        self.timeout = timeout
        self.cr = cr
        self.namespace = namespace
    def trigger(self):
        self.exec()
        self.wait()
        self.checkRuleSets()
    def exec(self):
        os.system(self.cmd)
    def wait(self):
        time.sleep(self.timeout)
    def checkRuleSets(self):
        for rs in self.ruleSets:
            rs.checkRuleSet(self.cr, self.namespace)

class SimpleMutator(Mutator):
    def mutate(self):
        for deltaSet in self.deltaSets:
            steps = deltaSet.find_path(self.baseCR)
            if steps:
                for delta in deltaSet.iterate():
                    yield self.apply_delta(self.baseCR, delta, steps)
            else:
                print("The path does not exist")

class HeadPodNameRule(Rule):
    def trigger_condition(self, cr) -> bool:
        steps = "spec.headGroupSpec".split('.')
        print("trigger_condition: " + (search_path(cr, steps) != None))
        return (search_path(cr, steps) != None)

    def assertRule(self, cr, namespace):
        expected_val = search_path(cr, "spec.headGroupSpec.template.spec.containers.0.name".split('.'))
        config.load_kube_config()
        v1 = client.CoreV1Api()
        headpods = v1.list_namespaced_pod(namespace = namespace, label_selector='rayNodeType={}'.format('head'))
        print("HeadPodNameRule: {} {}".format(expected_val, headpods.items[0].spec.containers[0].name))
        assert(headpods.items[0].spec.containers[0].name == expected_val)

def delete_kind_cluster():
    os.system("kind delete cluster")

def create_kind_cluster():
    os.system("kind create cluster")
    os.system("kubectl wait --for=condition=ready pod -n kube-system --all --timeout=900s")

def install_crd():
    KUBERAY_VERSION = "v0.3.0"
    os.system("kubectl create -k \"github.com/ray-project/kuberay/manifests/cluster-scope-resources?" +
              "ref={}&timeout=90s\"".format(KUBERAY_VERSION))

def download_images(images):
    client = docker.from_env()
    for image in images:
        client.images.pull(image)
    client.close()

def install_kuberay_operator():
    KUBERAY_VERSION = "v0.3.0"
    os.system("kubectl apply -k \"github.com/ray-project/kuberay/manifests/base?" +
              "ref={}&timeout=90s\"".format(KUBERAY_VERSION))
    time.sleep(60)

def kind_load_images(images):
    for image in images:
        os.system('kind load docker-image {}'.format(image))

if __name__ == '__main__':
    template_name = 'config/ray-cluster.mini.yaml.template'
    namespace = 'default'
    with open(template_name) as base_yaml:
        baseCR = yaml.load(base_yaml, Loader=yaml.FullLoader)
    ds = DeltaSet("spec.headGroupSpec.template.spec.containers.0.name", ['ray-head-1', 'ray-head-2', 'ray-head-3'])
    rs = RuleSet([HeadPodNameRule()])
    mut = SimpleMutator(baseCR, [ds])
    images = ['rayproject/ray:1.9.0', 'kuberay/operator:v0.3.0', 'kuberay/apiserver:v0.3.0']

    for cr in mut.mutate():
        delete_kind_cluster()
        create_kind_cluster()
        install_crd()
        download_images(images)
        kind_load_images(images)
        install_kuberay_operator()
        f = open('tmp.yaml', 'w')
        f.write(yaml.dump(cr))
        f.close()
        addEvent = CREvent(cr, "kubectl apply -f tmp.yaml", [rs], 90, namespace)
        addEvent.trigger()





