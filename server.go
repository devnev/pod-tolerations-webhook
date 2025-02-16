package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type mutationFunc func(admissionRequest *admissionv1.AdmissionRequest) (*admissionv1.AdmissionResponse, error)

func serve(w http.ResponseWriter, r *http.Request, m mutationFunc) {
	var (
		body []byte
		err  error
	)
	if body, err = io.ReadAll(r.Body); err != nil {
		http.Error(w, fmt.Sprintf("failed to read body: %v", err), http.StatusInternalServerError)
		return
	}
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	var admissionReviewReq admissionv1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admissionReviewReq); err != nil {
		http.Error(w, "failed to deserialize body", http.StatusBadRequest)
		return
	}
	if admissionReviewReq.Request == nil {
		http.Error(w, "missing admission request", http.StatusBadRequest)
		return
	}
	if admissionReviewReq.Request.Resource != podResource {
		http.Error(w, fmt.Sprintf("resource is not a pod, got %+v", admissionReviewReq.Request.Resource), http.StatusBadRequest)
		return
	}

	admissionResponse, err := m(admissionReviewReq.Request)
	if err != nil {
		slog.Info("Admission failed", errAttr(err))
		admissionResponse = &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: err.Error(),
				Status:  "Failure",
			},
		}
	}
	if admissionResponse == nil {
		http.Error(w, "admission produced nil response", http.StatusInternalServerError)
		return
	}

	admissionResponse.UID = admissionReviewReq.Request.UID
	admissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admissionv1.SchemeGroupVersion.String(),
			Kind:       "AdmissionReview",
		},
		Response: admissionResponse,
	}
	slog.Info("Admission review completed", slog.Any("review", admissionReview), slog.String("patch", string(admissionReview.Response.Patch)))

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, bytes.NewReader(resp)); err != nil {
		slog.Error("Failed to write response", errAttr(err))
		panic(http.ErrAbortHandler)
	}
}
