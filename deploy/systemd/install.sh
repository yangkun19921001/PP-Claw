#!/bin/bash
# ============================================================
# PP-Claw Linux systemd 服务管理脚本
# 用法: sudo ./install.sh {install|uninstall|start|stop|restart|status|logs}
# ============================================================

set -euo pipefail

SERVICE_NAME="pp-claw"
SERVICE_FILE="${SERVICE_NAME}.service"
INSTALL_BIN="/opt/pp-claw/pp-claw"
INSTALL_DIR="/opt/pp-claw"
SYSTEMD_DIR="/etc/systemd/system"
SERVICE_USER="ppclaw"
SERVICE_GROUP="ppclaw"

# 项目根目录 (脚本所在位置向上两级)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SERVICE_SRC="${SCRIPT_DIR}/${SERVICE_FILE}"

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
check_root() {
    if [[ $EUID -ne 0 ]]; then
        err "此脚本需要 root 权限，请使用 sudo 运行"
        exit 1
    fi
}

check_project() {
    if [[ ! -f "${PROJECT_ROOT}/Makefile" || ! -f "${PROJECT_ROOT}/main.go" ]]; then
        err "未找到 PP-Claw 项目根目录: ${PROJECT_ROOT}"
        err "请确保从项目目录运行此脚本"
        exit 1
    fi
}

is_active() {
    systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null
}

# ── install ───────────────────────────────────────────────────
do_install() {
    check_root
    check_project

    info "同步 vendor 依赖 ..."
    (cd "${PROJECT_ROOT}" && go mod tidy && go mod vendor)

    info "编译 PP-Claw ..."
    (cd "${PROJECT_ROOT}" && make build)
    echo ""

    # 创建用户
    if ! id "${SERVICE_USER}" &>/dev/null; then
        info "创建服务用户 ${SERVICE_USER} ..."
        useradd --system --shell /usr/sbin/nologin --home-dir "/home/${SERVICE_USER}" --create-home "${SERVICE_USER}"
        ok "用户已创建"
    fi

    # 安装二进制
    info "安装二进制到 ${INSTALL_DIR} ..."
    mkdir -p "${INSTALL_DIR}"
    cp "${PROJECT_ROOT}/pp-claw" "${INSTALL_BIN}"
    chmod +x "${INSTALL_BIN}"
    ok "二进制已安装"

    # 创建工作目录
    local workspace="/home/${SERVICE_USER}/.pp-claw"
    mkdir -p "${workspace}/logs" "${workspace}/workspace"
    chown -R "${SERVICE_USER}:${SERVICE_GROUP}" "${workspace}"

    # 安装 service 文件
    info "安装 systemd 服务 ..."
    cp "${SERVICE_SRC}" "${SYSTEMD_DIR}/${SERVICE_FILE}"
    systemctl daemon-reload
    ok "service 已安装到 ${SYSTEMD_DIR}/${SERVICE_FILE}"

    # 启用并启动
    systemctl enable "${SERVICE_NAME}"
    systemctl start "${SERVICE_NAME}"
    ok "PP-Claw 服务已安装并启动 (开机自启)"
    echo ""
    info "查看状态:  sudo $0 status"
    info "查看日志:  sudo $0 logs"
}

# ── uninstall ─────────────────────────────────────────────────
do_uninstall() {
    check_root

    info "卸载 PP-Claw 服务 ..."

    if is_active; then
        systemctl stop "${SERVICE_NAME}"
        ok "服务已停止"
    fi

    if systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null; then
        systemctl disable "${SERVICE_NAME}"
    fi

    if [[ -f "${SYSTEMD_DIR}/${SERVICE_FILE}" ]]; then
        rm -f "${SYSTEMD_DIR}/${SERVICE_FILE}"
        systemctl daemon-reload
        ok "service 文件已移除"
    fi

    if [[ -f "${INSTALL_BIN}" ]]; then
        rm -rf "${INSTALL_DIR}"
        ok "二进制已移除"
    fi

    ok "PP-Claw 服务已完全卸载"
    info "用户 ${SERVICE_USER} 及其数据保留，如需清理请手动执行:"
    info "  userdel -r ${SERVICE_USER}"
}

# ── start ─────────────────────────────────────────────────────
do_start() {
    check_root

    if ! [[ -f "${SYSTEMD_DIR}/${SERVICE_FILE}" ]]; then
        err "服务未安装，请先运行: sudo $0 install"
        exit 1
    fi

    if is_active; then
        warn "服务已在运行"
        do_status
        return
    fi

    info "启动 PP-Claw 服务 ..."
    systemctl start "${SERVICE_NAME}"
    ok "服务已启动"
}

# ── stop ──────────────────────────────────────────────────────
do_stop() {
    check_root

    if ! is_active; then
        warn "服务未在运行"
        return
    fi

    info "停止 PP-Claw 服务 ..."
    systemctl stop "${SERVICE_NAME}"
    ok "服务已停止"
}

# ── restart ───────────────────────────────────────────────────
do_restart() {
    check_root
    info "重启 PP-Claw 服务 ..."
    systemctl restart "${SERVICE_NAME}"
    ok "服务已重启"
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

    # systemd
    if [[ -f "${SYSTEMD_DIR}/${SERVICE_FILE}" ]]; then
        echo -e "  service:  ${GREEN}已安装${NC}"
    else
        echo -e "  service:  ${RED}未安装${NC}"
    fi

    # 运行状态
    if is_active; then
        local pid
        pid=$(systemctl show -p MainPID --value "${SERVICE_NAME}" 2>/dev/null || echo "")
        echo -e "  状态:     ${GREEN}运行中${NC} (PID: ${pid})"
    else
        echo -e "  状态:     ${RED}未运行${NC}"
    fi

    # 开机自启
    if systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null; then
        echo -e "  开机自启: ${GREEN}已启用${NC}"
    else
        echo -e "  开机自启: ${YELLOW}未启用${NC}"
    fi

    # 端口
    if command -v ss &>/dev/null; then
        local port_info
        port_info=$(ss -tlnp 2>/dev/null | grep ":18790" || true)
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
    info "实时日志 (Ctrl+C 退出) ..."
    journalctl -u "${SERVICE_NAME}" -f --no-pager
}

# ── upgrade ───────────────────────────────────────────────────
do_upgrade() {
    check_root
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
    echo "PP-Claw Linux 服务管理"
    echo ""
    echo "用法: sudo $0 <command>"
    echo ""
    echo "命令:"
    echo "  install     编译并安装为 systemd 服务 (开机自启)"
    echo "  uninstall   卸载服务并移除二进制"
    echo "  start       启动服务"
    echo "  stop        停止服务"
    echo "  restart     重启服务"
    echo "  status      查看服务状态"
    echo "  logs        查看实时日志 (journalctl)"
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
