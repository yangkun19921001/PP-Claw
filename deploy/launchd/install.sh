#!/bin/bash
# ============================================================
# PP-Claw macOS launchd 服务管理脚本
# 用法: ./install.sh {install|uninstall|start|stop|restart|status|logs}
# ============================================================

set -euo pipefail

SERVICE_LABEL="com.ppclaw.gateway"
PLIST_NAME="${SERVICE_LABEL}.plist"
INSTALL_BIN="/usr/local/bin/pp-claw"
LAUNCH_AGENTS_DIR="${HOME}/Library/LaunchAgents"
PLIST_DEST="${LAUNCH_AGENTS_DIR}/${PLIST_NAME}"
LOG_DIR="${HOME}/.pp-claw/logs"
STDOUT_LOG="${LOG_DIR}/pp-claw.stdout.log"
STDERR_LOG="${LOG_DIR}/pp-claw.stderr.log"

# 项目根目录 (脚本所在位置向上两级)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
PLIST_SRC="${SCRIPT_DIR}/${PLIST_NAME}"

# ── 颜色 ──────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# ── 检查 ──────────────────────────────────────────────────────
check_project() {
    if [[ ! -f "${PROJECT_ROOT}/Makefile" || ! -f "${PROJECT_ROOT}/main.go" ]]; then
        err "未找到 PP-Claw 项目根目录: ${PROJECT_ROOT}"
        err "请确保从项目目录运行此脚本"
        exit 1
    fi
}

is_loaded() {
    launchctl list "${SERVICE_LABEL}" &>/dev/null
}

get_pid() {
    launchctl list "${SERVICE_LABEL}" 2>/dev/null | awk 'NR==1{next} {print $1}' | grep -v '^-$' || true
}

# ── install ───────────────────────────────────────────────────
do_install() {
    check_project

    info "同步 vendor 依赖 ..."
    (cd "${PROJECT_ROOT}" && go mod tidy && go mod vendor)

    info "编译 PP-Claw ..."
    (cd "${PROJECT_ROOT}" && make build)
    echo ""

    info "安装二进制到 ${INSTALL_BIN} ..."
    cp "${PROJECT_ROOT}/pp-claw" "${INSTALL_BIN}"
    chmod +x "${INSTALL_BIN}"
    ok "二进制已安装"

    info "创建日志目录 ${LOG_DIR} ..."
    mkdir -p "${LOG_DIR}"

    info "安装 launchd 服务 ..."
    mkdir -p "${LAUNCH_AGENTS_DIR}"
    # 替换 plist 中的 HOME 占位符
    sed "s|PLACEHOLDER_HOME|${HOME}|g" "${PLIST_SRC}" > "${PLIST_DEST}"
    ok "plist 已安装到 ${PLIST_DEST}"

    # 如果已加载先卸载
    if is_loaded; then
        warn "服务已加载，先卸载旧版本 ..."
        launchctl bootout "gui/$(id -u)/${SERVICE_LABEL}" 2>/dev/null || true
    fi

    info "加载服务 ..."
    launchctl bootstrap "gui/$(id -u)" "${PLIST_DEST}"
    ok "PP-Claw 服务已安装并启动"
    echo ""
    info "查看状态:  $0 status"
    info "查看日志:  $0 logs"
}

# ── uninstall ─────────────────────────────────────────────────
do_uninstall() {
    info "卸载 PP-Claw 服务 ..."

    if is_loaded; then
        launchctl bootout "gui/$(id -u)/${SERVICE_LABEL}" 2>/dev/null || true
        ok "服务已停止"
    fi

    if [[ -f "${PLIST_DEST}" ]]; then
        rm -f "${PLIST_DEST}"
        ok "plist 已移除"
    fi

    if [[ -f "${INSTALL_BIN}" ]]; then
        rm -f "${INSTALL_BIN}"
        ok "二进制已移除"
    fi

    ok "PP-Claw 服务已完全卸载"
    info "配置和日志保留在 ~/.pp-claw/，如需清理请手动删除"
}

# ── start ─────────────────────────────────────────────────────
do_start() {
    if ! [[ -f "${PLIST_DEST}" ]]; then
        err "服务未安装，请先运行: $0 install"
        exit 1
    fi

    if is_loaded; then
        warn "服务已在运行"
        do_status
        return
    fi

    info "启动 PP-Claw 服务 ..."
    launchctl bootstrap "gui/$(id -u)" "${PLIST_DEST}"
    ok "服务已启动"
}

# ── stop ──────────────────────────────────────────────────────
do_stop() {
    if ! is_loaded; then
        warn "服务未在运行"
        return
    fi

    info "停止 PP-Claw 服务 ..."
    launchctl bootout "gui/$(id -u)/${SERVICE_LABEL}" 2>/dev/null || true
    ok "服务已停止"
}

# ── restart ───────────────────────────────────────────────────
do_restart() {
    do_stop
    sleep 1
    do_start
}

# ── status ────────────────────────────────────────────────────
do_status() {
    echo ""
    echo "═══════════════════════════════════════"
    echo "  PP-Claw 服务状态"
    echo "═══════════════════════════════════════"

    # 二进制
    if [[ -f "${INSTALL_BIN}" ]]; then
        local ver
        ver=$("${INSTALL_BIN}" version 2>/dev/null | head -1 || echo "unknown")
        echo -e "  二进制:   ${GREEN}已安装${NC} (${INSTALL_BIN})"
        echo -e "  版本:     ${ver}"
    else
        echo -e "  二进制:   ${RED}未安装${NC}"
    fi

    # plist
    if [[ -f "${PLIST_DEST}" ]]; then
        echo -e "  plist:    ${GREEN}已安装${NC}"
    else
        echo -e "  plist:    ${RED}未安装${NC}"
    fi

    # 运行状态
    if is_loaded; then
        local pid
        pid=$(get_pid)
        if [[ -n "${pid}" ]]; then
            echo -e "  状态:     ${GREEN}运行中${NC} (PID: ${pid})"
        else
            echo -e "  状态:     ${YELLOW}已加载，进程未启动${NC}"
        fi
    else
        echo -e "  状态:     ${RED}未运行${NC}"
    fi

    # 日志
    echo -e "  日志:     ${LOG_DIR}/"

    # 端口
    if command -v lsof &>/dev/null; then
        local port_info
        port_info=$(lsof -iTCP:18790 -sTCP:LISTEN -P -n 2>/dev/null | tail -1 || true)
        if [[ -n "${port_info}" ]]; then
            echo -e "  端口:     ${GREEN}18790 已监听${NC}"
        else
            echo -e "  端口:     ${YELLOW}18790 未监听${NC}"
        fi
    fi

    echo "═══════════════════════════════════════"
    echo ""
}

# ── logs ──────────────────────────────────────────────────────
do_logs() {
    if [[ ! -d "${LOG_DIR}" ]]; then
        err "日志目录不存在: ${LOG_DIR}"
        exit 1
    fi

    info "实时日志 (Ctrl+C 退出) ..."
    echo "--- stdout: ${STDOUT_LOG}"
    echo "--- stderr: ${STDERR_LOG}"
    echo ""
    tail -f "${STDOUT_LOG}" "${STDERR_LOG}" 2>/dev/null || {
        warn "日志文件尚未创建，服务可能未启动过"
    }
}

# ── upgrade ───────────────────────────────────────────────────
do_upgrade() {
    check_project

    info "升级 PP-Claw ..."

    info "拉取最新代码 ..."
    (cd "${PROJECT_ROOT}" && git pull)

    info "重新编译 ..."
    (cd "${PROJECT_ROOT}" && make build)

    info "替换二进制 ..."
    cp "${PROJECT_ROOT}/pp-claw" "${INSTALL_BIN}"
    chmod +x "${INSTALL_BIN}"

    do_restart
    ok "升级完成"
}

# ── main ──────────────────────────────────────────────────────
usage() {
    echo ""
    echo "PP-Claw macOS 服务管理"
    echo ""
    echo "用法: $0 <command>"
    echo ""
    echo "命令:"
    echo "  install     编译并安装为 launchd 服务 (开机自启)"
    echo "  uninstall   卸载服务并移除二进制"
    echo "  start       启动服务"
    echo "  stop        停止服务"
    echo "  restart     重启服务"
    echo "  status      查看服务状态"
    echo "  logs        查看实时日志"
    echo "  upgrade     拉取代码、重新编译、重启服务"
    echo ""
}

case "${1:-}" in
    install)    do_install   ;;
    uninstall)  do_uninstall ;;
    start)      do_start     ;;
    stop)       do_stop      ;;
    restart)    do_restart   ;;
    status)     do_status    ;;
    logs)       do_logs      ;;
    upgrade)    do_upgrade   ;;
    *)          usage        ;;
esac
