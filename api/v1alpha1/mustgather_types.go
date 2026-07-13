/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MustGatherSpec defines the desired state of MustGather
type MustGatherSpec struct {
	// the service account to use to run the must gather job pod, defaults to default
	// +kubebuilder:validation:Optional
	/* +kubebuilder:default:="{Name:default}" */
	ServiceAccountRef corev1.LocalObjectReference `json:"serviceAccountRef,omitempty"`

	// uploadTarget sets the target config for uploading the collected must-gather tar.
	// Uploading is disabled if this field is unset.
	// +optional
	UploadTarget *UploadTarget `json:"uploadTarget,omitempty"`

	// A flag to specify if audit logs must be collected
	// See documentation for further information.
	// +kubebuilder:default:=false
	Audit bool `json:"audit,omitempty"`

	// This represents the proxy configuration to be used. If left empty it will default to the cluster-level proxy configuration.
	// +kubebuilder:validation:Optional
	ProxyConfig ProxySpec `json:"proxyConfig,omitempty"`

	// A time limit for gather command to complete a floating point number with a suffix:
	// "s" for seconds, "m" for minutes, "h" for hours, or "d" for days.
	// Will default to no time limit.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Format=duration
	MustGatherTimeout metav1.Duration `json:"mustGatherTimeout,omitempty"`
}

// UploadType is a specific method for uploading to a target.
// +kubebuilder:validation:Enum=SFTP
type UploadType string

const (
	// UploadTypeSFTP uploads must-gather artifacts to an SFTP server.
	UploadTypeSFTP UploadType = "SFTP"
)

// UploadTarget defines the configuration for uploading the must-gather tar.
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'SFTP' ? has(self.sftp) : !has(self.sftp)",message="sftp upload target config is required when upload type is SFTP, and forbidden otherwise"
// +union
type UploadTarget struct {
	// type defines the method used for uploading to a specific target.
	// +unionDiscriminator
	// +required
	Type UploadType `json:"type"`

	// sftp defines the target details for uploading to a valid SFTP server.
	// +unionMember
	// +optional
	SFTP *SFTPUploadTargetConfig `json:"sftp,omitempty"`
}

// SFTPUploadTargetConfig defines the configuration for SFTP uploads.
type SFTPUploadTargetConfig struct {
	// caseID specifies the Red Hat case number for support uploads.
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:MinLength=1
	// +required
	CaseID string `json:"caseID"`

	// host specifies the SFTP server hostname.
	// +kubebuilder:default:="sftp.access.redhat.com"
	// +optional
	Host string `json:"host,omitempty"`

	// caseManagementAccountSecretRef references a secret containing the upload credentials.
	// +required
	CaseManagementAccountSecretRef corev1.LocalObjectReference `json:"caseManagementAccountSecretRef"`

	// A flag to specify if the upload user provided in the caseManagementAccountSecret is a RH internal user.
	// See documentation for further information.
	// +kubebuilder:default:=true
	// +optional
	InternalUser bool `json:"internalUser,omitempty"`
}

// +k8s:openapi-gen=true
type ProxySpec struct {
	// httpProxy is the URL of the proxy for HTTP requests.
	// +kubebuilder:validation:Required
	HTTPProxy string `json:"httpProxy"`

	// httpsProxy is the URL of the proxy for HTTPS requests.
	// +kubebuilder:validation:Required
	HTTPSProxy string `json:"httpsProxy"`

	// noProxy is the list of domains for which the proxy should not be used.  Empty means unset and will not result in an env var.
	// +optional
	NoProxy string `json:"noProxy,omitempty"`
}

// MustGatherStatus defines the observed state of MustGather
type MustGatherStatus struct {
	Status     string             `json:"status,omitempty"`
	LastUpdate metav1.Time        `json:"lastUpdate,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
	Completed  bool               `json:"completed"`
}

func (m *MustGather) GetConditions() []metav1.Condition {
	return m.Status.Conditions
}

func (m *MustGather) SetConditions(conditions []metav1.Condition) {
	m.Status.Conditions = conditions
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MustGather is the Schema for the mustgathers API
type MustGather struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MustGatherSpec   `json:"spec,omitempty"`
	Status MustGatherStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MustGatherList contains a list of MustGather
type MustGatherList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MustGather `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MustGather{}, &MustGatherList{})
}
