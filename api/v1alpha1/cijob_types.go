package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CIJobSpec defines the desired state of CIJob
type CIJobSpec struct {
	Image   string   `json:"image"`
	Command []string `json:"command"`
}

// CIJobStatus defines the observed state of CIJob
type CIJobStatus struct {
	Finished bool   `json:"finished"`
	Success  bool   `json:"success"`
	Canceled bool   `json:"canceled"`
	PodName  string `json:"podname"`
}

// +kubebuilder:object:root=true

// CIJob is the Schema for the cijobs API
type CIJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CIJobSpec   `json:"spec,omitempty"`
	Status CIJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CIJobList contains a list of CIJob
type CIJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CIJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CIJob{}, &CIJobList{})
}
