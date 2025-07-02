package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	yaml "gopkg.in/yaml.v2"
)

func getResourceRequirements(resources *ResourceRequirements) corev1.ResourceRequirements {
	if resources == nil {
		return corev1.ResourceRequirements{}
	}

	req := corev1.ResourceList{}
	lim := corev1.ResourceList{}

	// 处理请求资源
	if resources.Requests.CPU != "" {
		reqCpu := resource.MustParse(resources.Requests.CPU)
		req["cpu"] = reqCpu
	}
	if resources.Requests.Memory != "" {
		reqMem := resource.MustParse(resources.Requests.Memory)
		req["memory"] = reqMem
	}

	// 处理限制资源
	if resources.Limits.CPU != "" {
		limCpu := resource.MustParse(resources.Limits.CPU)
		lim["cpu"] = limCpu
	}
	if resources.Limits.Memory != "" {
		limMem := resource.MustParse(resources.Limits.Memory)
		lim["memory"] = limMem
	}

	return corev1.ResourceRequirements{
		Requests: req,
		Limits:   lim,
	}
}

// 构建依赖图
func buildDependencyGraph(deps JobPipelineConfig) (map[string][]string, map[string]int, map[string]bool) {
	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	allNodes := make(map[string]bool)

	for job := range deps.Jobs {
		allNodes[job] = true
		inDegree[job] = 0
	}

	for job, cfg := range deps.Jobs {
		for _, dep := range cfg.DependsOn {
			allNodes[dep] = true
			if _, exists := inDegree[dep]; !exists {
				inDegree[dep] = 0
			}
			graph[dep] = append(graph[dep], job)
			inDegree[job]++
		}
	}

	return graph, inDegree, allNodes
}

// 拓扑排序
func topologicalSort(graph map[string][]string, inDegree map[string]int, allNodes map[string]bool) ([]string, error) {
	queue := []string{}
	order := []string{}

	for node := range allNodes {
		if inDegree[node] == 0 {
			queue = append(queue, node)
		}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		for _, neighbor := range graph[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(order) != len(allNodes) {
		return nil, fmt.Errorf("存在循环依赖，无法完成所有任务")
	}

	return order, nil
}

// 等待依赖完成
func waitForDependencies(jobConfig JobConfig, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 添加初始日志，确认等待开始
	log.Printf("开始等待作业依赖: %v", jobConfig.DependsOn)

	// 使用ticker替代固定sleep，确保能响应ctx.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("等待依赖超时: %v, 未完成依赖: %v", ctx.Err(), jobConfig.DependsOn)
			return false
		case <-ticker.C:
			JobStatusLock.Lock()
			allDone := true
			var incompleteDeps []string

			for _, dep := range jobConfig.DependsOn {
				if !JobStatus[dep] {
					allDone = false
					incompleteDeps = append(incompleteDeps, dep)
				}
			}

			JobStatusLock.Unlock()

			if allDone {
				log.Printf("所有依赖已完成: %v", jobConfig.DependsOn)
				return true
			} else {
				// 添加未完成依赖日志，验证持续检查逻辑
				log.Printf("等待依赖中，未完成: %v", incompleteDeps)
			}
		}
	}
}

func CompleteJob(clientset *kubernetes.Clientset, pipelineConfig JobPipelineConfig, jobName string, job *batchv1.Job, resultChan chan<- error) {

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err := clientset.BatchV1().Jobs(pipelineConfig.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		resultChan <- fmt.Errorf("创建job %s 失败: %v", jobName, err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			resultChan <- fmt.Errorf("等待job %s 完成超时", jobName)
			return
		default:
			job, err := clientset.BatchV1().Jobs(pipelineConfig.Namespace).Get(ctx, jobName, metav1.GetOptions{})
			if err != nil {
				resultChan <- err
				return
			}
			if job.Status.Succeeded > 0 {
				log.Printf("job %s 完成", jobName)
				resultChan <- nil
				return
			} else if job.Status.Failed > 0 {
				resultChan <- fmt.Errorf("job %s failed", jobName)
				return
			}
			time.Sleep(2 * time.Second)
		}
	}
}

// 按拓扑排序顺序生成Job
func RUN(configmap InputConfigMap, jsonSerializer runtime.Serializer, pipelineConfig JobPipelineConfig, executionOrder []string, clientset *kubernetes.Clientset) {
	var wg sync.WaitGroup

	var jobConfigYAML string
	var jobConfig JobConfig
	var exists bool
	// 确保tmp目录存在
	if err := os.MkdirAll("out", 0700); err != nil {
		log.Printf("创建out目录失败: %v", err)
	}

	for _, jobName := range executionOrder {

		if os.Getenv("RUN_MODE") == "out-cluster" {
			if jobConfigYAML, exists = configmap.Data[jobName+".yaml"]; !exists {
				log.Printf("未找到%v.yaml配置文件", jobName)
				continue
			}
			if err := yaml.Unmarshal([]byte(jobConfigYAML), &jobConfig); err != nil {
				log.Printf("解析%v配置失败: %v", jobName, err)
				continue
			}
		}

		if os.Getenv("RUN_MODE") == "in-cluster" {
			jobConfigYAMLBytes, err := os.ReadFile("/work/config/" + jobName + ".yaml")
			if err != nil {
				log.Printf("读取%v.yaml失败: %v", jobName, err)
				continue
			}
			if err := yaml.Unmarshal(jobConfigYAMLBytes, &jobConfig); err != nil {
				log.Printf("解析%v配置失败: %v", jobName, err)
				continue
			}
		}

		jobConfig.DependsOn = pipelineConfig.Jobs[jobName].DependsOn

		//循环打印jobConfig.DependsOn
		for _, dep := range jobConfig.DependsOn {
			log.Printf("job %v depends on %v", jobName, dep)
		}

		if jobConfig.Image == "" {
			jobConfig.Image = pipelineConfig.Image
		}

		dnsConfig := &corev1.PodDNSConfig{}
		if pipelineConfig.DNSConfig != nil {
			dnsConfig.Options = pipelineConfig.DNSConfig.Options
		}

		// 在Job创建部分更新卷和卷挂载配置
		job := &batchv1.Job{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Job",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: pipelineConfig.Namespace,
				Annotations: map[string]string{
					"dependencies": strings.Join(jobConfig.DependsOn, ","),
				},
			},
			Spec: batchv1.JobSpec{
				Completions:           &pipelineConfig.Completions,
				Parallelism:           &pipelineConfig.Parallelism,
				ActiveDeadlineSeconds: &pipelineConfig.ActiveDeadlineSeconds,
				CompletionMode: func() *batchv1.CompletionMode {
					mode := batchv1.CompletionMode(pipelineConfig.CompletionMode)
					return &mode
				}(),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ImagePullSecrets: pipelineConfig.ImagePullSecrets,
						RestartPolicy:    corev1.RestartPolicy(pipelineConfig.RestartPolicy),
						DNSConfig:        dnsConfig,
						Volumes: func() []corev1.Volume {
							if jobConfig.Volumes == nil {
								return nil
							}
							var volumes []corev1.Volume
							for _, vol := range jobConfig.Volumes {
								volumes = append(volumes, corev1.Volume{
									Name: vol.Name,
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: vol.PersistentVolumeClaim.ClaimName,
										},
									},
								})
							}
							return volumes
						}(),
						Containers: []corev1.Container{
							{
								Name:      jobName,
								Image:     jobConfig.Image,
								Command:   jobConfig.Command,
								Resources: getResourceRequirements(jobConfig.Resources),
								VolumeMounts: func() []corev1.VolumeMount {
									if jobConfig.VolumeMounts == nil {
										return nil
									}
									var mounts []corev1.VolumeMount
									for _, mount := range jobConfig.VolumeMounts {
										mounts = append(mounts, corev1.VolumeMount{
											Name:      mount.Name,
											MountPath: mount.MountPath,
											ReadOnly:  mount.ReadOnly,
										})
									}
									return mounts
								}(),
							},
						},
					},
				},
			},
		}

		log.Printf("开始执行%s", jobName)
		OutPutJson(job, jobName, jsonSerializer)

		dependenciesCompleted := false
		for !dependenciesCompleted && RetryCount < MaxRetries {
			dependenciesCompleted = waitForDependencies(jobConfig, 3*time.Minute)
			if dependenciesCompleted {
				break
			}
			log.Printf("job %s等待依赖失败: %v (第 %d 次重试)", jobName, jobConfig.DependsOn, RetryCount+1)
			RetryCount++
		}
		if !dependenciesCompleted {
			log.Printf("job %s超过最大重试次数，依赖仍未满足", jobName)
			continue
		}

		resultChan := make(chan error, 1)
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			CompleteJob(clientset, pipelineConfig, name, job, resultChan)
			select {
			case result := <-resultChan:

				if result == nil {
					JobStatusLock.Lock()
					JobStatus[name] = true
					JobStatusLock.Unlock()
				} else {
					log.Printf("job %s执行失败: %v", name, result)
				}
			case <-time.After(10 * time.Minute):
				log.Printf("job %s执行超时", name)
			}
		}(jobName)
	}
	wg.Wait()
}

func CleanJobs(clientset *kubernetes.Clientset, namespace string, executionOrder []string) {
	deletePolicy := metav1.DeletePropagationForeground
	for _, jobName := range executionOrder {
		err := clientset.BatchV1().Jobs(namespace).Delete(context.Background(), jobName, metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		})
		if err != nil {
			log.Printf("删除job %s 失败: %v", jobName, err)
		} else {
			log.Printf("job %s 已删除", jobName)
		}
	}
}

func OutPutJson(job *batchv1.Job, jobName string, jsonSerializer runtime.Serializer) {
	jobJSON, err := runtime.Encode(jsonSerializer, job)
	if err != nil {
		log.Fatalf("序列化Job失败: %v", err)
	}
	outputPath := "out/" + jobName + "-job.json"
	if err := os.WriteFile(outputPath, jobJSON, 0644); err != nil {
		log.Printf("写入%v文件失败: %v", outputPath, err)
	}
}

func PrintExecutionOrder(executionOrder []string) {
	fmt.Println("任务执行顺序:")
	for i, job := range executionOrder {
		if i > 0 {
			fmt.Print(" → ")
		}
		fmt.Print(job)
	}
	fmt.Println()
}

func main() {
	var pipelineConfig JobPipelineConfig
	var inputConfigMap InputConfigMap
	if os.Getenv("RUN_MODE") == "" {
		configData, err := os.ReadFile("jobpipline.yaml")
		if err != nil {
			log.Fatalf("读取配置文件失败: %v", err)
		}

		if err = yaml.Unmarshal([]byte(configData), &inputConfigMap); err != nil {
			log.Fatalf("解析ConfigMap失败: %v", err)
		}

		jobsYAML := inputConfigMap.Data["jobs.yaml"]
		if jobsYAML == "" {
			log.Fatal("jobs.yaml内容为空")
		}

		if err = yaml.Unmarshal([]byte(jobsYAML), &pipelineConfig); err != nil {
			log.Fatalf("解析jobs.yaml失败: %v", err)
		}
	}

	if os.Getenv("RUN_MODE") == "in-cluster" {
		// 从/work/config文件夹加载jobs.yaml、job-*.yaml文件
		jobsYAML, err := os.ReadFile("/work/config/jobs.yaml")
		if err != nil {
			log.Fatalf("读取/work/config/jobs.yaml失败: %v", err)
		}
		if err = yaml.Unmarshal([]byte(jobsYAML), &pipelineConfig); err != nil {
			log.Fatalf("解析jobs.yaml失败: %v", err)
		}
	}

	graph, inDegree, allJobs := buildDependencyGraph(pipelineConfig)

	executionOrder, err := topologicalSort(graph, inDegree, allJobs)
	if err != nil {
		log.Fatalf("拓扑排序失败: %v", err)
	}

	PrintExecutionOrder(executionOrder)

	var config *rest.Config

	// default: out-cluster
	if os.Getenv("RUN_MODE") == "" {
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			log.Fatalf("获取用户主目录失败: %v", homeErr)
		}
		kubeconfigPath := filepath.Join(homeDir, ".kube", "config")

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			log.Fatalf("加载kubeconfig失败: %v", err)
		}
	}

	if os.Getenv("RUN_MODE") == "in-cluster" {
		// use 'default' serviceAccount' token or use workload' serviceAccount
		/*
			pod:
				tokenFile  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
				rootCAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		*/
		config, err = rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("创建Kubernetes客户端失败: %v", err)
	}

	var scheme = runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	var jsonSerializer = json.NewSerializer(json.DefaultMetaFactory, scheme, scheme, true)

	RUN(inputConfigMap, jsonSerializer, pipelineConfig, executionOrder, clientset)

	log.Printf("等待%v分钟后清理生成的job", pipelineConfig.CleanupTime)
	time.Sleep(time.Duration(pipelineConfig.CleanupTime) * time.Minute)
	// 清理生成的job
	CleanJobs(clientset, pipelineConfig.Namespace, executionOrder)
}
