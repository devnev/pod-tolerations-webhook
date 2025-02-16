package main

import (
	"encoding/json"
	"fmt"

	"gomodules.xyz/jsonpatch/v3"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
)

var toleration corev1.Toleration

func addToleration(admissionRequest *admissionv1.AdmissionRequest) (*admissionv1.AdmissionResponse, error) {
	raw := admissionRequest.Object.Raw
	pod := corev1.Pod{}

	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse pod: %w", err)
	}

	pod.Spec.Tolerations = append(pod.Spec.Tolerations, toleration)

	podWithToleration, err := json.Marshal(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal mutated pod: %w", err)
	}

	patchedOperation, err := jsonpatch.CreatePatch(raw, podWithToleration)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %w", err)
	}

	patchBytes, err := json.Marshal(patchedOperation)
	if err != nil {
		return nil, fmt.Errorf("failed to parse patch: %w", err)
	}

	patchType := admissionv1.PatchTypeJSONPatch
	admissionResponse := &admissionv1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}

	return admissionResponse, nil
}
