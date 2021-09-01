package utils

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBefore(t *testing.T) {
	if Before("a", "b") != "" {
		t.Fail()
	}

	if Before("aaa", "a") != "" {
		t.Fail()
	}

	if Before("aab", "b") != "aa" {
		t.Fail()
	}
}

func TestStatus(t *testing.T) {
	pod := createSomePod()
	pod.Status.Phase = corev1.PodPending
	if !IsCreated(pod) {
		t.Fail()
	}
}

func TestIsValid(t *testing.T) {
	podName := "bad name" //  white space
	_, NameIsValid := IsValid(podName)
	if NameIsValid {
		t.Fail()
	}

	podName = "goodName"
	_, NameIsValid = IsValid(podName)
	if !NameIsValid {
		t.Fail()
	}

	podName = "badName-" // ends with dash
	_, NameIsValid = IsValid(podName)
	if NameIsValid {
		t.Fail()
	}

	podName = "good54367name"
	_, NameIsValid = IsValid(podName)
	if !NameIsValid {
		t.Fail()
	}

	podName = "bad.name." // ends with dot
	_, NameIsValid = IsValid(podName)
	if NameIsValid {
		t.Fail()
	}

	podName = "bad.name.@yes" // has special char
	_, NameIsValid = IsValid(podName)
	if NameIsValid {
		t.Fail()
	}

	podName = "tooooooooooooooooooooooooooooooolonnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnng" // too long
	_, NameIsValid = IsValid(podName)
	if NameIsValid {
		t.Fail()
	}
}

func TestTrimName(t *testing.T) {
	podName := "tooooooooooooooooooooooooooooooolonnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnng" // too long
	_, NameIsValid := IsValid(podName)
	if !NameIsValid {
		podName = TrimName(podName)
		fmt.Println(podName)
		if len(podName) > 56 {
			t.Fail()
		}
	}
	podName = "toooooooooooooooooooo--------------------------------"
	errorList, NameIsValid := IsValid(podName)
	if !NameIsValid {
		podName = TrimName(podName)
		fmt.Println(podName, strings.Join(errorList, " "))
		if len(podName) > 56 {
			t.Fail()
		}
	}
	podName = "------------------------------------------------123456789--------------------------------"
	errorList, NameIsValid = IsValid(podName)
	if !NameIsValid {
		podName = TrimName(podName)
		fmt.Println(podName, strings.Join(errorList, " "))
		if len(podName) > 9 {
			t.Fail()
		}
	}
}

func TestTrimMap(t *testing.T) {
	myMap := map[string]string{
		"identifier": "f14805df-6edb-06d9-e8e3-ecfd05c4c1ae-lazer090scholar-director-head",
	}
	myMap = TrimMap(myMap)
	if len(myMap["identifier"]) > 56 {
		t.Fail()
	}
}

func createSomePod() (pod *corev1.Pod) {

	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample-small-group-worker-0",
			Namespace: "default",
		},
	}
}
