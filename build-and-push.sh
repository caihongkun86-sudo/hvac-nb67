#!/usr/bin/env bash
# =============================================================
# build-and-push.sh
# MACDA Connector 镜像一键构建 & 推送脚本
#
# 用法:
#   ./build-and-push.sh              # 构建并推送所有服务（使用默认版本）
#   ./build-and-push.sh v1.2.0       # 构建并推送所有服务（指定版本）
#   ./build-and-push.sh v1.2.0 web   # 只构建并推送 web 服务
#   ./build-and-push.sh v1.2.0 bff   # 只构建并推送 bff 服务
#   ./build-and-push.sh v1.2.0 reporter  # 只构建并推送 ground-reporter 服务
# =============================================================

set -euo pipefail  # 错误即退出，未定义变量报错，管道错误传递

# ── 配置区（按需修改）────────────────────────────────────────
REGISTRY="harbor.naivehero.top:8443/macda2"
VERSION="${1:-v1.0.0}"       # 第1个参数为版本号，默认 v1.0.0
TARGET="${2:-all}"            # 第2个参数为构建目标，默认 all

# 脚本所在目录的上一级（= 项目根目录）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${SCRIPT_DIR}"

# 各服务目录
WEB_DIR="${PROJECT_ROOT}/web-nb67-web"
BFF_DIR="${PROJECT_ROOT}/web-nb67-bff"
CONNECT_DIR="${PROJECT_ROOT}/connect"

# 镜像全名
WEB_IMAGE="${REGISTRY}/nb67-web:${VERSION}"
BFF_IMAGE="${REGISTRY}/nb67-bff:${VERSION}"
REPORTER_IMAGE="${REGISTRY}/ground-reporter:${VERSION}"

# ── 工具函数 ─────────────────────────────────────────────────
log_info()  { echo -e "\033[0;32m[INFO]\033[0m  $*"; }
log_warn()  { echo -e "\033[0;33m[WARN]\033[0m  $*"; }
log_error() { echo -e "\033[0;31m[ERROR]\033[0m $*" >&2; }
log_step()  { echo -e "\n\033[1;36m══════ $* ══════\033[0m"; }

# ── 前置检查 ─────────────────────────────────────────────────
check_prerequisites() {
    log_step "前置检查"
    
    if ! command -v docker &>/dev/null; then
        log_error "未找到 docker 命令，请先安装 Docker"
        exit 1
    fi
    
    if ! docker info &>/dev/null; then
        log_error "Docker daemon 未运行，请先启动 Docker"
        exit 1
    fi
    
    log_info "Docker 就绪 ✓"
    log_info "目标版本: ${VERSION}"
    log_info "镜像仓库: ${REGISTRY}"
    log_info "构建目标: ${TARGET}"
}

# ── 构建 Web 前端镜像 ─────────────────────────────────────────
build_web() {
    log_step "构建 nb67-web 镜像"
    
    if [[ ! -d "${WEB_DIR}" ]]; then
        log_error "目录不存在: ${WEB_DIR}"
        exit 1
    fi
    
    log_info "镜像: ${WEB_IMAGE}"
    log_info "构建目录: ${WEB_DIR}"
    
    docker build \
        --platform linux/amd64 \
        --tag "${WEB_IMAGE}" \
        --file "${WEB_DIR}/Dockerfile" \
        "${WEB_DIR}"
    
    log_info "nb67-web 构建完成 ✓"
}

# ── 构建 BFF 镜像 ─────────────────────────────────────────────
build_bff() {
    log_step "构建 nb67-bff 镜像"
    
    if [[ ! -d "${BFF_DIR}" ]]; then
        log_error "目录不存在: ${BFF_DIR}"
        exit 1
    fi
    
    log_info "镜像: ${BFF_IMAGE}"
    log_info "构建目录: ${BFF_DIR}"
    
    docker build \
        --platform linux/amd64 \
        --tag "${BFF_IMAGE}" \
        --file "${BFF_DIR}/Dockerfile" \
        "${BFF_DIR}"
    
    log_info "nb67-bff 构建完成 ✓"
}

# ── 构建 ground-reporter 镜像 ─────────────────────────────────
build_reporter() {
    log_step "构建 ground-reporter 镜像"

    if [[ ! -d "${CONNECT_DIR}" ]]; then
        log_error "目录不存在: ${CONNECT_DIR}"
        exit 1
    fi

    log_info "镜像: ${REPORTER_IMAGE}"
    log_info "构建上下文: ${CONNECT_DIR}"

    docker build \
        --platform linux/amd64 \
        --tag "${REPORTER_IMAGE}" \
        --file "${CONNECT_DIR}/Dockerfile.ground-reporter" \
        "${CONNECT_DIR}"

    log_info "ground-reporter 构建完成 ✓"
}

# ── 推送镜像到 Harbor ─────────────────────────────────────────
push_image() {
    local image="$1"
    log_info "推送镜像: ${image}"
    
    docker push "${image}"
    log_info "推送完成 ✓ → ${image}"
}

# ── 打印摘要 ─────────────────────────────────────────────────
print_summary() {
    log_step "构建摘要"
    echo ""
    echo "  版本:     ${VERSION}"
    
    if [[ "${TARGET}" == "all" || "${TARGET}" == "web" ]]; then
        echo "  Web镜像:  ${WEB_IMAGE}"
        docker images "${WEB_IMAGE}" --format "           大小: {{.Size}}  创建: {{.CreatedAt}}" 2>/dev/null || true
    fi
    
    if [[ "${TARGET}" == "all" || "${TARGET}" == "bff" ]]; then
        echo "  BFF镜像:  ${BFF_IMAGE}"
        docker images "${BFF_IMAGE}" --format "           大小: {{.Size}}  创建: {{.CreatedAt}}" 2>/dev/null || true
    fi

    if [[ "${TARGET}" == "all" || "${TARGET}" == "reporter" ]]; then
        echo "  Reporter镜像: ${REPORTER_IMAGE}"
        docker images "${REPORTER_IMAGE}" --format "           大小: {{.Size}}  创建: {{.CreatedAt}}" 2>/dev/null || true
    fi
    
    echo ""
    echo "  部署命令:"
    echo "    cd baseEnv"
    echo "    docker compose -f docker-compose-Prod.yml pull"
    echo "    docker compose -f docker-compose-Prod.yml up -d"
    echo ""
}

# ── 主流程 ───────────────────────────────────────────────────
main() {
    log_step "MACDA Connector 镜像构建"
    
    check_prerequisites
    
    case "${TARGET}" in
        "all")
            build_web
            build_bff
            build_reporter
            push_image "${WEB_IMAGE}"
            push_image "${BFF_IMAGE}"
            push_image "${REPORTER_IMAGE}"
            ;;
        "web")
            build_web
            # push_image "${WEB_IMAGE}"
            ;;
        "bff")
            build_bff
            # push_image "${BFF_IMAGE}"
            ;;
        "reporter")
            build_reporter
            push_image "${REPORTER_IMAGE}"
            ;;
        *)
            log_error "未知构建目标: ${TARGET}，可选值: all | web | bff | reporter"
            exit 1
            ;;
    esac
    
    print_summary
    log_info "全部完成！🎉"
}

main "$@"
