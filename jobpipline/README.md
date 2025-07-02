# jobpipline

## 简介：

基于kubernetes  client-go库实现相互依赖job任务编排

## 代码构建：

```
bash build/build.sh

out:.
├── in-cluster
│   └── jobpipline-1.0.0.tar.gz
└── out-of-cluster
    ├── jobpipline
    └── jobpipline.yaml
```


#### kubernetes集群外执行:

```
# 机器已配置kubeconfig，默认读取“~/.kube/config”
./jobpipline
```

#### kubernetes集群内执行:

```
cd in-cluster
tar -zxf jobpipline-1.0.0.tar.gz

.
├── charts
│   └── jobpipline-1.0.0.tgz
├── images
│   └── jobpipline-1.0.0.tar
```


#### 上传镜像

docker load -i images/jobpipline-1.0.0.tar

安装

helm install jobpipline charts/jobpipline-1.0.0.tgz

卸载

helm list

helm uninstall jobpipline -n default


案例任务执行过程：

kubectl  get pod | grep job
job-a-xfbd4                               0/1     Completed   0              84s
job-b-t57rz                               0/1     Completed   0              119s
job-c-f4c9s                               0/1     Completed   0              2m39s
job-d-tnf8d                               0/1     Completed   0              2m34s
jobpipline-job-xpnmx                      1/1     Running     0              3m11s

kubectl logs -f jobpipline-job-xpnmx
任务执行顺序:
job-c → job-d → job-b → job-a
2025/07/02 03:45:09 开始执行job-c
2025/07/02 03:45:09 开始等待作业依赖: []
2025/07/02 03:45:14 所有依赖已完成: []
2025/07/02 03:45:14 开始执行job-d
2025/07/02 03:45:14 开始等待作业依赖: []
2025/07/02 03:45:19 所有依赖已完成: []
2025/07/02 03:45:19 job job-b depends on job-c
2025/07/02 03:45:19 job job-b depends on job-d
2025/07/02 03:45:19 开始执行job-b
2025/07/02 03:45:19 开始等待作业依赖: [job-c job-d]
2025/07/02 03:45:24 等待依赖中，未完成: [job-c job-d]
2025/07/02 03:45:29 等待依赖中，未完成: [job-c job-d]
2025/07/02 03:45:34 等待依赖中，未完成: [job-c job-d]
2025/07/02 03:45:39 等待依赖中，未完成: [job-c job-d]
2025/07/02 03:45:44 等待依赖中，未完成: [job-c job-d]
2025/07/02 03:45:44 job job-c 完成
2025/07/02 03:45:49 等待依赖中，未完成: [job-d]
2025/07/02 03:45:49 job job-d 完成
2025/07/02 03:45:54 所有依赖已完成: [job-c job-d]
2025/07/02 03:45:54 job job-a depends on job-b
2025/07/02 03:45:54 开始执行job-a
2025/07/02 03:45:54 开始等待作业依赖: [job-b]
2025/07/02 03:45:59 等待依赖中，未完成: [job-b]
2025/07/02 03:46:04 等待依赖中，未完成: [job-b]
2025/07/02 03:46:09 等待依赖中，未完成: [job-b]
2025/07/02 03:46:14 等待依赖中，未完成: [job-b]
2025/07/02 03:46:19 等待依赖中，未完成: [job-b]
2025/07/02 03:46:24 等待依赖中，未完成: [job-b]
2025/07/02 03:46:24 job job-b 完成
2025/07/02 03:46:29 所有依赖已完成: [job-b]
2025/07/02 03:46:59 job job-a 完成
2025/07/02 03:46:59 等待5分钟后清理生成的job
2025/07/02 03:51:59 job job-c 已删除
2025/07/02 03:51:59 job job-d 已删除
2025/07/02 03:51:59 job job-b 已删除
2025/07/02 03:51:59 job job-a 已删除
