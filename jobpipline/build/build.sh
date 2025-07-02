#!/bin/bash

WORKDIR=$(cd `dirname $0`/../;pwd)
OUTPUT_DIR="${WORKDIR}/out"
IN_CLUSTER_DIR="${OUTPUT_DIR}/in-cluster"
OUT_OF_CLUSTER_DIR="${OUTPUT_DIR}/out-of-cluster"
VERSION=${1:-1.0.0}

function before_start() {
    if ! command -v helm &> /dev/null; then
        echo "错误: 未找到helm命令，请先安装Helm"
        exit 1
    fi
}

function prepare_go_mod() {
    export GO111MODULE=on
    export GOPROXY=https://goproxy.cn,direct
    export GONOMOD=*
    export GOSUMDB=off
}

function create_directories() {
    pushd "${WORKDIR}"
    echo "创建输出目录结构..."
    rm -rf "${OUTPUT_DIR}"
    mkdir -p "${IN_CLUSTER_DIR}" "${OUT_OF_CLUSTER_DIR}"
    popd
}

function build_binary() {
    pushd "${WORKDIR}"
    echo "构建二进制文件..."
    go mod tidy
    GOOS=linux GOARCH=amd64 go build -o "${OUT_OF_CLUSTER_DIR}/jobpipline" "${WORKDIR}/cmd/jobpipline"
    popd
}

function copy_config_files() {
    pushd "${WORKDIR}"
    echo "复制配置文件..."
    cp cmd/jobpipline/jobpipline.yaml "${OUT_OF_CLUSTER_DIR}/"
    sed -i 's/runmode: .*/runmode: "out-of-cluster"/g' "${OUT_OF_CLUSTER_DIR}/jobpipline.yaml"

    cp -rf build/charts "${IN_CLUSTER_DIR}/" 
    cp cmd/jobpipline/jobpipline.yaml "${IN_CLUSTER_DIR}/charts/jobpipline/templates/"
    sed -i 's/runmode: .*/runmode: "in-cluster"/g' "${IN_CLUSTER_DIR}/charts/jobpipline/templates/jobpipline.yaml"
    popd
}

function buildDockerImage() {
    pushd "${OUT_OF_CLUSTER_DIR}"
    echo "生成Dockerfile..."
    cat > Dockerfile <<EOF
FROM alpine:latest
WORKDIR /work
COPY jobpipline /work/

CMD ["./jobpipline"]
EOF
    echo "构建Docker镜像..."
    docker build -t jobpipline:${VERSION} .
    mkdir -p "${IN_CLUSTER_DIR}/images"
    docker save jobpipline:${VERSION} > ${IN_CLUSTER_DIR}/images/jobpipline-${VERSION}.tar
    rm -rf Dockerfile 
    docker rmi -f jobpipline:${VERSION}
    popd
}

# 准备Helm Chart
function prepare_helm_chart() {
    pushd "${IN_CLUSTER_DIR}/charts"
    echo "准备Helm chart..."
    sed -i 's/version: .*/version: '"${VERSION}"'/g' jobpipline/Chart.yaml
    helm package jobpipline -d "${IN_CLUSTER_DIR}/charts/"
    rm -rf jobpipline
    popd
}

function package_prd() {
    pushd "${IN_CLUSTER_DIR}"
    echo "准备生产包..."
    tar -czvf jobpipline-${VERSION}.tar.gz charts images
    rm -rf charts images
    popd
}


function main() {
    before_start
    prepare_go_mod
    create_directories
    build_binary
    copy_config_files
    buildDockerImage
    prepare_helm_chart
    package_prd
}

# 执行主函数
main
