package v1alpha1

import (
	"errors"
	"fmt"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ErrValidation is for validation errors
var ErrValidation = errors.New("validation error")

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CIJobSpec defines the desired state of CIJob
type CIJobSpec struct {
	Image      string          `json:"image"`
	Command    []string        `json:"command"`
	Repository CIJobRepository `json:"repository"`
	WorkingDir string          `json:"workdir"`
}

// Validate ensures all the parts work
func (spec CIJobSpec) Validate() error {
	lenCheck := map[string]int{
		"image":   len(spec.Image),
		"command": len(spec.Command),
		"workdir": len(spec.WorkingDir),
	}

	for key, length := range lenCheck {
		if length == 0 {
			return fmt.Errorf("%s missing: %w", key, ErrValidation)
		}
	}

	if err := spec.Repository.Validate(); err != nil {
		return fmt.Errorf("repository info invalid: %w", err)
	}

	return nil
}

// CIJobRepository represents a repository that needs to be cloned for the test to run
type CIJobRepository struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Token    string `json:"token"`
}

// Validate validates the repository information
func (repo *CIJobRepository) Validate() error {
	lenCheck := map[string]int{
		"url":      len(repo.URL),
		"token":    len(repo.Token),
		"username": len(repo.Username),
	}

	for key, length := range lenCheck {
		if length == 0 {
			return fmt.Errorf("%s missing: %w", key, ErrValidation)
		}
	}

	u, err := url.Parse(repo.URL)
	if err != nil {
		return fmt.Errorf("clone url %q invalid: %w", repo.URL, err)
	}

	if u.Scheme != "https" {
		return fmt.Errorf("clone url only supports https at this time: %w", ErrValidation)
	}

	return nil
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

// Pod returns a pod with a spec relative to this CIJob.
func (job *CIJob) Pod(nsName types.NamespacedName) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName.Namespace,
			Name:      nsName.Name,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "ci-run",
					Image:   job.Spec.Image,
					Command: job.Spec.Command,
				},
			},
		},
	}
}

// Validate is a convenience function to validate the spec (for now)
func (job *CIJob) Validate() error {
	return job.Spec.Validate()
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
