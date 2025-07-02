package main

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
)

var (
	// 设置最大重试次数防止死循环
	MaxRetries = 5
	RetryCount = 0
)

var (
	JobStatusLock sync.Mutex
	JobStatus     = make(map[string]bool)
)

type JobPipelineConfig struct {
	RunMode               string                        `yaml:"runmode"`
	CleanupTime           int64                         `yaml:"cleanuptime"`
	Namespace             string                        `yaml:"namespace"`
	Image                 string                        `yaml:"image"`
	Completions           int32                         `yaml:"completions"`
	Parallelism           int32                         `yaml:"parallelism"`
	ActiveDeadlineSeconds int64                         `yaml:"activeDeadlineSeconds"`
	ImagePullSecrets      []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty"`
	RestartPolicy         string                        `yaml:"restartPolicy"`
	DNSConfig             *corev1.PodDNSConfig          `yaml:"dnsConfig,omitempty"`
	CompletionMode        string                        `yaml:"completionMode"`
	Jobs                  map[string]JobDep             `yaml:"jobs"`
}

type JobDep struct {
	DependsOn []string `yaml:"depends_on"`
}

type JobConfig struct {
	Command      []string              `yaml:"command,omitempty"`
	Image        string                `yaml:"image,omitempty"`
	Resources    *ResourceRequirements `yaml:"resources,omitempty"`
	Volumes      []VolumeConfig        `yaml:"volumes,omitempty"`
	VolumeMounts []VolumeMountConfig   `yaml:"volumeMounts,omitempty"`
	DependsOn    []string              `yaml:"depends_on,omitempty"`
}

type InputConfigMap struct {
	Data map[string]string `yaml:"data"`
}

// 资源需求配置
type ResourceRequirements struct {
	Limits   *ResourceConfig `yaml:"limits,omitempty"`
	Requests *ResourceConfig `yaml:"requests,omitempty"`
}

// 资源配置
type ResourceConfig struct {
	CPU    string `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// 卷配置
type VolumeConfig struct {
	Name                  string `yaml:"name"`
	PersistentVolumeClaim struct {
		ClaimName string `yaml:"claimName"`
	} `yaml:"persistentVolumeClaim"`
}

// 卷挂载配置
type VolumeMountConfig struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly,omitempty"`
}

type K8sJob struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       JobSpec  `yaml:"spec"`
}

type Metadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type JobSpec struct {
	Template PodTemplate `yaml:"template"`
}

type PodTemplate struct {
	Spec PodSpec `yaml:"spec"`
}

type PodSpec struct {
	Containers    []Container `yaml:"containers"`
	RestartPolicy string      `yaml:"restartPolicy"`
}

type Container struct {
	Name         string                `yaml:"name"`
	Image        string                `yaml:"image"`
	Command      []string              `yaml:"command"`
	Resources    *ResourceRequirements `yaml:"resources,omitempty"`
	VolumeMounts []VolumeMountConfig   `yaml:"volumeMounts,omitempty"`
}
