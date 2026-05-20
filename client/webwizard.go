package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// webWizardHTML is the embedded setup page served when the user double-clicks ai-monitor.exe.
const webWizardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AI Token 监控</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  /* ── 自定义滚动条：与暗色玻璃 UI 一致的细圆角槽 ───────────────────── */
  *::-webkit-scrollbar { width: 10px; height: 10px; }
  *::-webkit-scrollbar-track { background: rgba(15,23,42,0.4); border-radius: 8px; }
  *::-webkit-scrollbar-thumb { background: linear-gradient(180deg, #475569 0%, #334155 100%); border-radius: 8px; border: 2px solid rgba(30,41,59,0); background-clip: padding-box; }
  *::-webkit-scrollbar-thumb:hover { background: linear-gradient(180deg, #64748b 0%, #475569 100%); background-clip: padding-box; border: 2px solid rgba(30,41,59,0); }
  *::-webkit-scrollbar-corner { background: transparent; }
  * { scrollbar-width: thin; scrollbar-color: #475569 rgba(15,23,42,0.4); }
  body { font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif; background: #0f172a; color: #e2e8f0; min-height: 100vh; padding: 16px; display: flex; align-items: stretch; justify-content: center; gap: 16px; }
  .card { background: #1e293b; border-radius: 16px; box-shadow: 0 25px 50px rgba(0,0,0,0.5); padding: 40px; width: 860px; max-width: 96vw; flex: 0 0 auto; overflow-y: auto; max-height: calc(100vh - 32px); }
  h1 { font-size: 24px; text-align: center; margin-bottom: 8px; color: #38bdf8; }
  .subtitle { text-align: center; color: #94a3b8; margin-bottom: 28px; font-size: 14px; }
  .field { margin-bottom: 18px; }
  .field label { display: block; font-size: 13px; color: #94a3b8; margin-bottom: 6px; font-weight: 500; }
  .field input, .field select { width: 100%; padding: 10px 14px; border: 1px solid #334155; border-radius: 8px; background: #0f172a; color: #e2e8f0; font-size: 14px; outline: none; transition: border-color 0.2s; }
  .field input:focus { border-color: #38bdf8; }
  .field input::placeholder { color: #475569; }
  .field .hint { font-size: 12px; color: #64748b; margin-top: 4px; }
  .row { display: flex; gap: 12px; }
  .row .field { flex: 1; }
  button { width: 100%; padding: 12px; border: none; border-radius: 8px; font-size: 15px; font-weight: 600; cursor: pointer; transition: all 0.2s; }
  .btn-primary { background: linear-gradient(135deg, #0ea5e9, #6366f1); color: white; margin-top: 8px; }
  .btn-primary:hover { opacity: 0.9; transform: translateY(-1px); }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; transform: none; }
  .btn-secondary { background: transparent; border: 1px solid #475569; color: #94a3b8; margin-top: 8px; }
  .btn-secondary:hover { border-color: #38bdf8; color: #e2e8f0; }
  /* ── Toast 通知：右上角悬浮，自动隐藏，不再被底部内容遮挡 ────────── */
  #status { position: fixed; top: 18px; right: 18px; z-index: 9999; max-width: 460px; min-width: 240px; padding: 12px 16px; border-radius: 10px; font-size: 13px; display: none; line-height: 1.6; box-shadow: 0 12px 32px rgba(0,0,0,0.55); backdrop-filter: blur(6px); opacity: 0; transform: translateY(-8px); transition: opacity 0.22s, transform 0.22s; pointer-events: auto; }
  #status.show { opacity: 1; transform: translateY(0); }
  #status.success { display: block; background: rgba(6,78,59,0.95); color: #6ee7b7; border: 1px solid #10b981; }
  #status.error { display: block; background: rgba(69,10,10,0.95); color: #fca5a5; border: 1px solid #ef4444; }
  #status.info { display: block; background: rgba(30,58,95,0.95); color: #93c5fd; border: 1px solid #3b82f6; }
  .logo { text-align: center; margin-bottom: 16px; font-size: 40px; }
  .advanced-toggle { text-align: center; margin: 12px 0; }
  .advanced-toggle a { color: #64748b; font-size: 12px; cursor: pointer; text-decoration: none; }
  .advanced-toggle a:hover { color: #94a3b8; }
  .advanced { display: none; }
  .advanced.show { display: block; }
  .step { display: none; }
  .step.active { display: block; }
  .tabs { display: flex; gap: 0; margin-bottom: 24px; border-bottom: 2px solid #334155; }
  .tab { flex: 1; padding: 10px 0; text-align: center; font-size: 14px; font-weight: 600; color: #64748b; cursor: pointer; border-bottom: 2px solid transparent; margin-bottom: -2px; transition: all 0.2s; }
  .tab:hover { color: #94a3b8; }
  .tab.active { color: #38bdf8; border-bottom-color: #38bdf8; }
  .auth-form { display: none; }
  .auth-form.active { display: block; }
  .auth-msg { margin-top: 12px; padding: 10px; border-radius: 8px; font-size: 13px; display: none; }
  .auth-msg.error { display: block; background: #450a0a; color: #fca5a5; border: 1px solid #7f1d1d; }
  .auth-msg.success { display: block; background: #064e3b; color: #6ee7b7; border: 1px solid #065f46; }
  .user-info { background: #0f172a; border: 1px solid #334155; border-radius: 8px; padding: 16px; margin-bottom: 20px; }
  .user-info .user-name { font-size: 16px; font-weight: 600; color: #38bdf8; }
  .user-info .user-detail { font-size: 13px; color: #94a3b8; margin-top: 4px; }
  .step-indicator { display: flex; justify-content: center; gap: 8px; margin-bottom: 24px; }
  .step-dot { width: 8px; height: 8px; border-radius: 50%; background: #334155; transition: background 0.3s; }
  .step-dot.active { background: #38bdf8; }
  .panel { border: 1px solid #334155; border-radius: 10px; padding: 14px; margin-bottom: 12px; }
  .panel-title { font-size: 13px; color: #94a3b8; margin-bottom: 10px; }
  .btn-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .btn-grid-3 { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .btn-small { margin-top: 0; padding: 10px; font-size: 13px; }
  .btn-small.active { border: 1px solid #38bdf8; box-shadow: 0 0 0 1px #38bdf8 inset; }
  .mono { font-family: Consolas, "Courier New", monospace; font-size: 12px; color: #cbd5e1; }
  .status-bar { background: #0b1220; border: 1px solid #243244; border-radius: 10px; padding: 10px 12px; margin-bottom: 12px; line-height: 1.7; }
  .log-panel { border: 1px solid #334155; border-radius: 16px; background: #1e293b; box-shadow: 0 25px 50px rgba(0,0,0,0.5); flex: 1 1 auto; min-width: 360px; max-width: 100%; display: flex; flex-direction: column; max-height: calc(100vh - 32px); overflow: hidden; }
  .log-head { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid #334155; flex: 0 0 auto; }
  .log-head .lbl { font-size: 14px; color: #e2e8f0; font-weight: 600; }
  .log-head .lbl .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #475569; margin-right: 6px; vertical-align: middle; transition: background 0.2s; }
  .log-head .lbl.live .dot { background: #22c55e; box-shadow: 0 0 6px #22c55e; }
  .log-head .actions { display: flex; gap: 6px; align-items: center; }
  .log-head .actions button { width: auto; margin: 0; padding: 4px 10px; font-size: 12px; font-weight: 500; background: transparent; border: 1px solid #334155; color: #94a3b8; border-radius: 6px; }
  .log-head .actions button:hover { color: #e2e8f0; border-color: #475569; }
  .log-head .actions label { font-size: 12px; color: #94a3b8; display: flex; align-items: center; gap: 4px; cursor: pointer; }
  .log-body { font-family: Consolas, "Courier New", monospace; font-size: 12px; color: #cbd5e1; line-height: 1.6; padding: 10px 14px; flex: 1 1 auto; overflow-y: auto; white-space: pre-wrap; word-break: break-all; background: #0b1220; border-radius: 0 0 16px 16px; }
  .log-body > div { padding: 1px 0; }
  .log-body > div:hover { background: rgba(56,189,248,0.05); }
  .log-body .ts { color: #475569; margin-right: 8px; }
  .log-body .tag { display: inline-block; padding: 0 6px; margin-right: 6px; border-radius: 4px; font-size: 11px; font-weight: 600; letter-spacing: 0.3px; line-height: 1.55; }
  .log-body .tag[data-tag="usage"]      { background: #0c4a6e; color: #7dd3fc; }
  .log-body .tag[data-tag="记录"]       { background: #14532d; color: #86efac; }
  .log-body .tag[data-tag="MITM"],
  .log-body .tag[data-tag="MITM/h2"]    { background: #3b0764; color: #c4b5fd; }
  .log-body .tag[data-tag="CONNECT"]    { background: #1e293b; color: #94a3b8; border: 1px solid #334155; }
  .log-body .tag[data-tag="上报"]       { background: #7c2d12; color: #fdba74; }
  .log-body .tag[data-tag="wizard"],
  .log-body .tag[data-tag="认证"]       { background: #831843; color: #f9a8d4; }
  .log-body .tag[data-tag="提示"],
  .log-body .tag[data-tag="启动"]       { background: #365314; color: #bef264; }
  .log-body .tag.tag-default            { background: #1e293b; color: #94a3b8; }
  .log-body .k     { color: #94a3b8; }
  .log-body .num   { color: #fbbf24; font-weight: 600; }
  .log-body .model { color: #38bdf8; }
  .log-body .app   { color: #c4b5fd; }
  .log-body .url   { color: #67e8f9; }
  .log-body .st-2  { color: #4ade80; font-weight: 600; }
  .log-body .st-3  { color: #fbbf24; font-weight: 600; }
  .log-body .st-4  { color: #fb923c; font-weight: 600; }
  .log-body .st-5  { color: #f87171; font-weight: 600; }
  .log-body .ln-err{ color: #fca5a5; }
  .range-tabs { display: flex; gap: 6px; background: #0b1220; border: 1px solid #243244; border-radius: 10px; padding: 4px; margin-bottom: 12px; }
  .range-tabs button { flex: 1; padding: 8px 6px; background: transparent; border: none; color: #94a3b8; font-size: 13px; font-weight: 500; border-radius: 6px; cursor: pointer; transition: all 0.2s; margin: 0; }
  .range-tabs button:hover { color: #e2e8f0; }
  .range-tabs button.active { background: linear-gradient(135deg, rgba(220,38,38,0.5), rgba(124,45,18,0.5)); color: #fecaca; }
  .stats-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-bottom: 12px; }
  .stat-card { background: #0b1220; border: 1px solid #243244; border-radius: 12px; padding: 14px 16px; position: relative; overflow: hidden; }
  .stat-card::before { content: ''; position: absolute; top: 0; left: 0; width: 3px; height: 100%; }
  .stat-card.red::before    { background: linear-gradient(180deg, #f97316, #ef4444); }
  .stat-card.green::before  { background: linear-gradient(180deg, #10b981, #14b8a6); }
  .stat-card.amber::before  { background: linear-gradient(180deg, #f59e0b, #f97316); }
  .stat-card.blue::before   { background: linear-gradient(180deg, #3b82f6, #06b6d4); }
  .stat-card .stat-label { font-size: 12px; color: #94a3b8; margin-bottom: 6px; }
  .stat-card .stat-num   { font-size: 26px; font-weight: 700; letter-spacing: 0.5px; font-family: Consolas, "Courier New", monospace; }
  .stat-card.red   .stat-num { color: #fb923c; }
  .stat-card.green .stat-num { color: #4ade80; }
  .stat-card.amber .stat-num { color: #fbbf24; }
  .stat-card.blue  .stat-num { color: #60a5fa; }
  /* ── 一键启动按钮：图标 + 名称 + 悬浮位移 ───────────────────────── */
  .launch-section { margin-bottom: 14px; }
  .launch-section:last-of-type { margin-bottom: 0; }
  .launch-section-title { font-size: 12px; color: #64748b; margin-bottom: 8px; padding-left: 2px; letter-spacing: 0.5px; }
  .launch-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
  .launch-btn { display: flex; align-items: center; gap: 10px; padding: 10px 12px; background: #0b1220; border: 1px solid #243244; border-radius: 10px; color: #e2e8f0; cursor: pointer; transition: all 0.18s; font-size: 13px; font-weight: 500; text-align: left; width: 100%; margin: 0; min-height: 48px; }
  .launch-btn:hover { border-color: #38bdf8; background: #0e1830; transform: translateY(-1px); box-shadow: 0 4px 12px rgba(56,189,248,0.18); }
  .launch-btn:active { transform: translateY(0); }
  .launch-ico { width: 30px; height: 30px; border-radius: 8px; display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0; box-shadow: inset 0 0 0 1px rgba(255,255,255,0.08); overflow: hidden; }
  .launch-ico svg { width: 18px; height: 18px; display: block; }
  .launch-name { flex: 1; line-height: 1.2; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  @media (max-width: 760px) {
    .launch-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 1100px) {
    body { flex-direction: column; align-items: center; }
    .log-panel { width: 860px; max-width: 96vw; max-height: 320px; flex: 0 0 auto; }
  }
  @media (max-width: 1100px) {
    .btn-grid, .btn-grid-3 { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 700px) {
    .btn-grid, .btn-grid-3 { grid-template-columns: 1fr; }
  }
</style>
</head>
<body>
<div class="card">
  <div class="logo">🔍</div>
  <h1>AI Token 监控</h1>
  <p class="subtitle">安装后自动记录所有开发工具的 AI Token 使用量</p>
  <div class="step-indicator">
    <div class="step-dot active" id="dot1"></div>
    <div class="step-dot" id="dot2"></div>
  </div>

  <!-- ========== Step 1: 认证 ========== -->
  <div class="step active" id="step1">
    <div class="field">
      <label>上报服务器</label>
      <input id="serverUrl" type="text" value="{{.ServerURL}}" placeholder="{{.ServerURL}}" />
      <div class="hint">公司内部部署的统计服务器地址</div>
    </div>

    <div class="tabs">
      <div class="tab active" onclick="switchTab('login')">登录</div>
      <div class="tab" onclick="switchTab('register')">注册</div>
      <div class="tab" onclick="switchTab('resetpwd')">忘记密码</div>
    </div>

    <!-- 注册表单 -->
    <div class="auth-form" id="registerForm">
      <div class="field">
        <label>姓名 *</label>
        <input id="regName" type="text" value="{{.UserName}}" required placeholder="真实姓名" />
      </div>
      <div class="field">
        <label>邮箱 *</label>
        <input id="regEmail" type="email" placeholder="例如：zhangsan@company.com" />
      </div>
      <div class="field">
        <label>部门</label>
        <input id="regDept" type="text" placeholder="例如：公共技术部" />
      </div>
      <div class="row">
        <div class="field">
          <label>密码 *</label>
          <input id="regPwd" type="password" placeholder="至少6位" />
        </div>
        <div class="field">
          <label>确认密码 *</label>
          <input id="regPwd2" type="password" placeholder="再次输入" />
        </div>
      </div>
      <button class="btn-primary" id="regBtn" onclick="doRegister()">注册</button>
      <div style="text-align:right;margin-top:8px;"><a style="color:#38bdf8;font-size:12px;cursor:pointer;" onclick="switchTab('login')">已有账号？去登录</a></div>
      <div class="auth-msg" id="regMsg"></div>
    </div>

    <!-- 登录表单 -->
    <div class="auth-form active" id="loginForm">
      <div class="field">
        <label>邮箱</label>
        <input id="loginId" type="text" placeholder="注册时使用的邮箱" />
      </div>
      <div class="field">
        <label>密码</label>
        <input id="loginPwd" type="password" placeholder="密码" />
      </div>
      <button class="btn-primary" id="loginBtn" onclick="doLogin()">登录</button>
      <div style="display:flex;justify-content:space-between;margin-top:8px;">
        <a style="color:#38bdf8;font-size:12px;cursor:pointer;" onclick="switchTab('register')">没有账号？去注册</a>
        <a style="color:#64748b;font-size:12px;cursor:pointer;" onclick="switchTab('resetpwd')">忘记密码？</a>
      </div>
      <div class="auth-msg" id="loginMsg"></div>
    </div>

    <!-- 忘记密码表单 -->
    <div class="auth-form" id="resetpwdForm">
      <div class="hint" style="margin-bottom:16px;color:#94a3b8;">忘记密码？输入你的邮箱和注册时的姓名验证身份，即可设置新密码。</div>
      <div class="row">
        <div class="field">
          <label>邮箱 *</label>
          <input id="rpEmail" type="text" placeholder="注册时使用的邮箱" />
        </div>
        <div class="field">
          <label>姓名 *</label>
          <input id="rpName" type="text" placeholder="与账号对应的姓名" />
        </div>
      </div>
      <div class="row">
        <div class="field">
          <label>新密码 *</label>
          <input id="rpNewPwd" type="password" placeholder="至少6位" />
        </div>
        <div class="field">
          <label>确认新密码 *</label>
          <input id="rpNewPwd2" type="password" placeholder="再次输入" />
        </div>
      </div>
      <button class="btn-primary" id="rpBtn" onclick="doResetPassword()">重置密码</button>
      <div style="text-align:right;margin-top:8px;"><a style="color:#38bdf8;font-size:12px;cursor:pointer;" onclick="switchTab('login')">想起密码？返回登录</a></div>
      <div class="auth-msg" id="rpMsg"></div>
    </div>
  </div>

  <!-- ========== Step 2: 安装 ========== -->
  <div class="step" id="step2">
    <div class="user-info">
      <div class="user-name" id="displayName"></div>
      <div class="user-detail" id="displayDetail"></div>
    </div>
    {{if not .FirstInstall}}<div class="status-bar mono" id="quickStatus">模式: 未知 | 上游: (direct)</div>

    <div class="panel">
      <div class="panel-title">使用统计</div>
      <div class="range-tabs">
        <button id="rangeBtn1" class="active" onclick="setRange(1)">今日</button>
        <button id="rangeBtn7" onclick="setRange(7)">近7天</button>
        <button id="rangeBtn15" onclick="setRange(15)">近15天</button>
        <button id="rangeBtn30" onclick="setRange(30)">近30天</button>
      </div>
      <div class="stats-grid">
        <div class="stat-card red">
          <div class="stat-label" id="statTokensLabel">今日 Tokens</div>
          <div class="stat-num" id="statTokens">—</div>
        </div>
        <div class="stat-card green">
          <div class="stat-label" id="statRequestsLabel">今日请求</div>
          <div class="stat-num" id="statRequests">—</div>
        </div>
        <div class="stat-card amber">
          <div class="stat-label" id="statCostLabel">今日成本</div>
          <div class="stat-num" id="statCost">—</div>
        </div>
        <div class="stat-card blue">
          <div class="stat-label" id="statUsersLabel">缓存命中率</div>
          <div class="stat-num" id="statUsers">—</div>
          <div class="stat-sub" id="statUsersSub" style="font-size:12px;color:#94a3b8;margin-top:4px;">读 0 / 写 0</div>
        </div>
      </div>
    </div>

    <div class="panel">
      <div class="panel-title">一键模式切换</div>
      <div class="btn-grid">
        <button class="btn-secondary btn-small" id="modeObserveBtn" onclick="switchMode('observe')" title="不改系统代理 / PAC / 环境变量。仅本程序运行时监听端口；只有 --launch 子进程或手动把应用代理指到本程序端口的流量才会被监控。本机保持「干净」。">观察模式</button>
        <button class="btn-secondary btn-small" id="modeSessionBtn" onclick="switchMode('session_pac')" title="写一份 AI 域名白名单 PAC（仅 AI 相关域名走本程序，其它网站照常）。本程序退出时自动还原。Electron 类 IDE（VS Code / Cursor）改 PAC 后通常要重启，可用下方「一键启动」帮你拉。【日常推荐】">会话PAC模式</button>
        <button class="btn-secondary btn-small" id="modeGlobalBtn" onclick="switchMode('global_install')" title="写 PAC + 用户级代理环境变量等（类似全局安装），关掉本程序后仍然接管。适合希望新开终端 / 多数软件默认就走监控路径的场景。停用要点「一键恢复网络」或运行 --global-uninstall / 卸载脚本。">全局接管模式</button>
        <button class="btn-secondary btn-small" id="modeCleanupBtn" onclick="switchMode('cleanup')" title="一次性清理 PAC / 代理 / 用户级环境变量等接管残留，回到「未接管」状态。网络异常或想彻底回退时点。">一键恢复网络</button>
      </div>
      <div class="hint" id="modeHint" style="margin-top:8px;">当前模式: 未获取</div>
    </div>

    <div class="panel">
      <div class="panel-title">一键启动编辑器 / 终端（已启用自动重启同名运行实例）</div>

      <div class="launch-section">
        <div class="launch-section-title">AI 编辑器 / CLI</div>
        <div class="launch-grid">
          <button class="launch-btn" onclick="launchPreset('cursor')" title="启动 Cursor">
            <span class="launch-ico" style="background:linear-gradient(135deg,#000 0%,#1f1f1f 100%);color:#fff;font-weight:700;font-size:14px;">C</span>
            <span class="launch-name">Cursor</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('vscode')" title="启动 VS Code">
            <span class="launch-ico" style="background:#0a66c2;">
              <svg viewBox="0 0 24 24" fill="#fff"><path d="M17.5 2.5l-11 9.5 5 4.5L17.5 2.5zM3 8l3.5 3.5L3 15l1.5 1.5L8.5 13l9 8.5L21 19V5l-3.5-2.5L8.5 11 4.5 7 3 8z"/></svg>
            </span>
            <span class="launch-name">VS Code</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('codex')" title="在 PowerShell 中启动 Codex CLI，自动注入本地 MITM 环境变量。">
            <span class="launch-ico" style="background:linear-gradient(135deg,#10a37f 0%,#0e8a6b 100%);">
              <svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><polyline points="8 9 5 12 8 15"/><polyline points="16 9 19 12 16 15"/></svg>
            </span>
            <span class="launch-name">Codex</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('windsurf')" title="启动 Windsurf">
            <span class="launch-ico" style="background:linear-gradient(135deg,#0ea5a4 0%,#0891b2 100%);color:#fff;font-weight:700;font-size:14px;">W</span>
            <span class="launch-name">Windsurf</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('kiro')" title="启动 Kiro">
            <span class="launch-ico" style="background:linear-gradient(135deg,#7c3aed 0%,#a855f7 100%);color:#fff;font-weight:700;font-size:14px;">K</span>
            <span class="launch-name">Kiro</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('trae')" title="启动 Trae">
            <span class="launch-ico" style="background:linear-gradient(135deg,#f43f5e 0%,#e11d48 100%);color:#fff;font-weight:700;font-size:14px;">T</span>
            <span class="launch-name">Trae</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('qoder')" title="启动 Qoder（阿里 AI IDE）">
            <span class="launch-ico" style="background:linear-gradient(135deg,#f97316 0%,#ea580c 100%);color:#fff;font-weight:700;font-size:14px;">Q</span>
            <span class="launch-name">Qoder</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('zed')" title="启动 Zed Editor">
            <span class="launch-ico" style="background:linear-gradient(135deg,#0ea5e9 0%,#0369a1 100%);color:#fff;font-weight:700;font-size:14px;">Z</span>
            <span class="launch-name">Zed</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('claude-code')" title="在 PowerShell 中启动 Claude Code CLI，自动注入本地 MITM 环境变量。">
            <span class="launch-ico" style="background:linear-gradient(135deg,#d97706 0%,#b45309 100%);color:#fff;font-weight:700;font-size:14px;">CC</span>
            <span class="launch-name">Claude Code</span>
          </button>
        </div>
      </div>

      <div class="launch-section">
        <div class="launch-section-title">终端</div>
        <div class="launch-grid">
          <button class="launch-btn" onclick="launchPreset('powershell')" title="启动 PowerShell">
            <span class="launch-ico" style="background:#012456;">
              <svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="5 7 10 12 5 17"/><line x1="13" y1="17" x2="19" y2="17"/></svg>
            </span>
            <span class="launch-name">PowerShell</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('cmd')" title="启动 CMD">
            <span class="launch-ico" style="background:#1e293b;">
              <svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 8 10 12 6 16"/><line x1="12" y1="16" x2="18" y2="16"/></svg>
            </span>
            <span class="launch-name">CMD</span>
          </button>
        </div>
      </div>

      <div class="launch-section">
        <div class="launch-section-title">JetBrains 系列</div>
        <div class="launch-grid">
          <button class="launch-btn" onclick="launchPreset('idea')" title="启动 IntelliJ IDEA">
            <span class="launch-ico" style="background:linear-gradient(135deg,#087cfa 0%,#fe315d 50%,#f97a12 100%);color:#fff;font-weight:700;font-size:13px;">IJ</span>
            <span class="launch-name">IntelliJ IDEA</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('webstorm')" title="启动 WebStorm">
            <span class="launch-ico" style="background:linear-gradient(135deg,#00cdd7 0%,#087cfa 50%,#fff200 100%);color:#000;font-weight:700;font-size:13px;">WS</span>
            <span class="launch-name">WebStorm</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('pycharm')" title="启动 PyCharm">
            <span class="launch-ico" style="background:linear-gradient(135deg,#21d789 0%,#fcf84a 50%,#07c3f2 100%);color:#000;font-weight:700;font-size:13px;">PC</span>
            <span class="launch-name">PyCharm</span>
          </button>
          <button class="launch-btn" onclick="launchPreset('goland')" title="启动 GoLand">
            <span class="launch-ico" style="background:linear-gradient(135deg,#0eb9c3 0%,#bf3bb4 50%,#3bea62 100%);color:#fff;font-weight:700;font-size:13px;">GL</span>
            <span class="launch-name">GoLand</span>
          </button>
        </div>
      </div>
    </div>
    {{end}}

    <div id="setupActions">
      <div class="advanced-toggle"><a onclick="toggleAdvanced()">▶ 高级选项</a></div>
      <div class="advanced" id="advancedSection">
        <div class="field">
          <label>上游代理（高级，可选）</label>
          <input id="upstreamProxy" type="text" placeholder="通常留空；必须串接公司/本地代理时才填写" />
          <div class="hint">默认不依赖上游地址，按本机网络直连</div>
        </div>
        <div class="field">
          <label>监听端口</label>
          <input id="port" type="number" value="18090" min="1024" max="65535" />
        </div>
      </div>

      <button class="btn-primary" id="installBtn" onclick="doInstall()">一键安装</button>
    </div>
    <div id="status"></div>
  </div>
</div>

<div class="log-panel" id="logPanel">
  <div class="log-head">
    <div class="lbl" id="logLbl"><span class="dot"></span>运行日志</div>
  </div>
  <div class="log-body" id="logBody"></div>
</div>

<script>
var authUser = null;
var basePath = '{{.BasePath}}';
var wizardToken = '{{.WizardToken}}';
var consoleStatus = null;

function wizardHeaders(extra) {
  var headers = extra || {};
  if (wizardToken) headers['X-AI-Monitor-Wizard-Token'] = wizardToken;
  return headers;
}

var statusHideTimer = null;
function setStatus(level, text) {
  var status = document.getElementById('status');
  status.className = level;
  status.style.display = 'block';
  status.textContent = text;
  // 触发 reflow 以确保 transition 生效
  void status.offsetWidth;
  status.classList.add('show');
  if (statusHideTimer) { clearTimeout(statusHideTimer); statusHideTimer = null; }
  // info 短一点（操作进行中会被后续 success/error 覆盖），success 4s，error 7s 留出阅读时间
  var hold = level === 'error' ? 7000 : (level === 'info' ? 6000 : 4000);
  statusHideTimer = setTimeout(function() {
    status.classList.remove('show');
    setTimeout(function() {
      if (!status.classList.contains('show')) {
        status.style.display = 'none';
        status.className = '';
      }
    }, 260);
  }, hold);
}
// 点击 toast 立即关闭
document.addEventListener('DOMContentLoaded', function() {
  var s = document.getElementById('status');
  if (s) s.addEventListener('click', function() {
    s.classList.remove('show');
    setTimeout(function() { s.style.display = 'none'; s.className = ''; }, 260);
    if (statusHideTimer) { clearTimeout(statusHideTimer); statusHideTimer = null; }
  });
});

function activateModeButton(mode) {
  ['modeObserveBtn', 'modeSessionBtn', 'modeGlobalBtn', 'modeCleanupBtn'].forEach(function(id) {
    var el = document.getElementById(id);
    if (el) el.classList.remove('active');
  });
  var id = '';
  if (mode === 'observe') id = 'modeObserveBtn';
  if (mode === 'session') id = 'modeSessionBtn';
  if (mode === 'persistent') id = 'modeGlobalBtn';
  if (id) {
    var btn = document.getElementById(id);
    if (btn) btn.classList.add('active');
  }
}

function modeText(mode) {
  if (mode === 'observe') return '观察模式（不接管网络）';
  if (mode === 'session' || mode === 'session_pac') return '会话PAC模式（退出自动还原）';
  if (mode === 'persistent' || mode === 'global_install') return '全局接管模式（持久）';
  if (mode === 'cleanup') return '一键恢复网络';
  return mode || '未知';
}

function shortMode(mode) {
  if (mode === 'observe') return '观察';
  if (mode === 'session') return '会话PAC';
  if (mode === 'persistent') return '全局';
  return mode || '未知';
}

async function refreshConsoleStatus() {
  try {
    var resp = await fetch(basePath + '/api/console/status');
    if (!resp.ok) return false;
    var data = await resp.json();
    if (!data || !data.success || !data.supported) return false;
    consoleStatus = data;
    activateModeButton(data.mode);
    var name = data.user_name || '';
    var uid = data.user_id || '';
    var dept = data.department || '';
    var detail = '服务器：' + (data.server_url || '');
    if (dept) detail = '部门：' + dept + '  ' + detail;
    document.getElementById('displayName').textContent = name && uid ? (name + '（' + uid + '）') : (name || '当前用户');
    document.getElementById('displayDetail').textContent = detail;
    document.getElementById('modeHint').textContent = '当前模式: ' + modeText(data.mode) + ' | 已上报: ' + (data.total_reported || 0);
    var upstream = data.upstream_proxy || '(direct)';
    document.getElementById('quickStatus').textContent = '模式: ' + shortMode(data.mode) + ' | 上游: ' + upstream;
    return true;
  } catch (err) {
    return false;
  }
  return false;
}

var modeSwitchInFlight = false;

function setModeButtonsDisabled(disabled) {
  ['modeObserveBtn','modeSessionBtn','modeGlobalBtn','modeCleanupBtn'].forEach(function(id){
    var el = document.getElementById(id);
    if (el) el.disabled = disabled;
  });
}

async function switchMode(mode) {
  if (modeSwitchInFlight) {
    setStatus('info', '上一次切换还在进行，请稍候 ...');
    return;
  }
  var label = modeText(mode);
  if (mode === 'global_install' && !confirm('确认切换到全局接管模式？这会写入持久网络设置。')) return;
  if (mode === 'cleanup' && !confirm('确认执行一键恢复网络？这会清理当前代理接管设置。')) return;

  modeSwitchInFlight = true;
  setModeButtonsDisabled(true);
  var startedAt = Date.now();
  var elapsedTimer = setInterval(function(){
    var sec = Math.floor((Date.now() - startedAt) / 1000);
    setStatus('info', '正在切换模式：' + label + ' ...（已耗时 ' + sec + 's）');
  }, 1000);
  setStatus('info', '正在切换模式：' + label + ' ...');

  try {
    var resp = await fetch(basePath + '/api/console/mode', {
      method: 'POST',
      headers: wizardHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ mode: mode }),
    });
    var data = {};
    try { data = await resp.json(); } catch(e) { data = {}; }
    var took = ((Date.now() - startedAt) / 1000).toFixed(1) + 's';
    if (resp.ok && data.success) {
      setStatus('success', '✓ ' + data.message + '（' + took + '）');
    } else {
      setStatus('error', '✗ ' + (data.message || ('HTTP ' + resp.status)) + '（' + took + '）');
    }
  } catch (err) {
    setStatus('error', '✗ 网络错误: ' + err.message);
  } finally {
    clearInterval(elapsedTimer);
    modeSwitchInFlight = false;
    setModeButtonsDisabled(false);
    // 无论成功失败都强制刷新一次状态，避免 UI 停在旧模式。
    try { await refreshConsoleStatus(); } catch(e) {}
  }
}

async function launchPreset(name) {
  setStatus('info', '正在启动：' + name + ' ...');
  try {
    var resp = await fetch(basePath + '/api/console/launch', {
      method: 'POST',
      headers: wizardHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ preset: name, restart_running: true }),
    });
    var data = await resp.json();
    if (resp.ok && data.success) {
      setStatus('success', '✓ ' + data.message);
    } else {
      setStatus('error', '✗ ' + (data.message || '启动失败'));
    }
  } catch (err) {
    setStatus('error', '✗ 网络错误: ' + err.message);
  }
}

function switchTab(tab) {
  document.querySelectorAll('.tab').forEach(function(t){ t.classList.remove('active'); });
  document.querySelectorAll('.auth-form').forEach(function(f){ f.classList.remove('active'); });
  var tabs = document.querySelectorAll('.tabs .tab');
  if (tab === 'login') {
    tabs[0].classList.add('active');
    document.getElementById('loginForm').classList.add('active');
  } else if (tab === 'register') {
    tabs[1].classList.add('active');
    document.getElementById('registerForm').classList.add('active');
  } else {
    tabs[2].classList.add('active');
    document.getElementById('resetpwdForm').classList.add('active');
  }
  hideMsg('regMsg'); hideMsg('loginMsg'); hideMsg('rpMsg');
}

function showMsg(id, text, level) {
  var el = document.getElementById(id);
  el.className = 'auth-msg ' + level;
  el.textContent = text;
}
function hideMsg(id) {
  var el = document.getElementById(id);
  el.className = 'auth-msg';
  el.style.display = 'none';
  el.textContent = '';
}

function getServerUrl() {
  return document.getElementById('serverUrl').value.trim().replace(/\/+$/, '');
}

async function doRegister() {
  var name = document.getElementById('regName').value.trim();
  var email = document.getElementById('regEmail').value.trim();
  var dept = document.getElementById('regDept').value.trim();
  var pwd = document.getElementById('regPwd').value;
  var pwd2 = document.getElementById('regPwd2').value;
  if (!name) { showMsg('regMsg', '请填写姓名', 'error'); return; }
  if (!email) { showMsg('regMsg', '请填写邮箱', 'error'); return; }
  if (!pwd || pwd.length < 6) { showMsg('regMsg', '密码至少6位', 'error'); return; }
  if (pwd !== pwd2) { showMsg('regMsg', '两次密码不一致', 'error'); return; }
  if (!getServerUrl()) { showMsg('regMsg', '请填写上报服务器地址', 'error'); return; }

  var btn = document.getElementById('regBtn');
  btn.disabled = true; btn.textContent = '注册中…';
  hideMsg('regMsg');

  try {
    var resp = await fetch(basePath + '/api/auth/register', {
      method: 'POST',
      headers: wizardHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ server_url: getServerUrl(), name: name, email: email, department: dept, password: pwd }),
    });
    var data = await resp.json();
    if (resp.ok && data.employee_id) {
      authUser = { employee_id: data.employee_id, name: data.name || name, department: data.department || dept, auth_token: data.auth_token || '' };
      showMsg('regMsg', '注册成功！', 'success');
      setTimeout(function(){ goToStep2(); }, 800);
    } else {
      showMsg('regMsg', data.detail || data.message || '注册失败', 'error');
    }
  } catch(err) {
    showMsg('regMsg', '网络错误: ' + err.message, 'error');
  }
  btn.disabled = false; btn.textContent = '注册';
}

async function doLogin() {
  var id = document.getElementById('loginId').value.trim();
  var pwd = document.getElementById('loginPwd').value;
  if (!id || !pwd) { showMsg('loginMsg', '请填写邮箱和密码', 'error'); return; }
  if (!getServerUrl()) { showMsg('loginMsg', '请填写上报服务器地址', 'error'); return; }

  var btn = document.getElementById('loginBtn');
  btn.disabled = true; btn.textContent = '登录中…';
  hideMsg('loginMsg');

  try {
    var resp = await fetch(basePath + '/api/auth/login', {
      method: 'POST',
      headers: wizardHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ server_url: getServerUrl(), email: id, password: pwd }),
    });
    var data = await resp.json();
    if (resp.ok && data.employee_id) {
      authUser = { employee_id: data.employee_id, name: data.name || '', department: data.department || '', auth_token: data.auth_token || '' };
      showMsg('loginMsg', '登录成功', 'success');
      setTimeout(function(){ goToStep2(); }, 500);
    } else if (resp.status === 403 && data.detail === 'password_not_set') {
      showMsg('loginMsg', '该账号尚未设置密码，请先注册或联系管理员', 'error');
    } else {
      showMsg('loginMsg', data.detail || data.message || '登录失败', 'error');
    }
  } catch(err) {
    showMsg('loginMsg', '网络错误: ' + err.message, 'error');
  }
  btn.disabled = false; btn.textContent = '登录';
}

async function doResetPassword() {
  var email = document.getElementById('rpEmail').value.trim();
  var name = document.getElementById('rpName').value.trim();
  var newPwd = document.getElementById('rpNewPwd').value;
  var newPwd2 = document.getElementById('rpNewPwd2').value;
  if (!email) { showMsg('rpMsg', '请填写邮箱', 'error'); return; }
  if (!name) { showMsg('rpMsg', '请填写姓名', 'error'); return; }
  if (!newPwd || newPwd.length < 6) { showMsg('rpMsg', '新密码至少6位', 'error'); return; }
  if (newPwd !== newPwd2) { showMsg('rpMsg', '两次新密码不一致', 'error'); return; }
  if (!getServerUrl()) { showMsg('rpMsg', '请填写上报服务器地址', 'error'); return; }

  var btn = document.getElementById('rpBtn');
  btn.disabled = true; btn.textContent = '重置中…';
  hideMsg('rpMsg');

  try {
    var resp = await fetch(basePath + '/api/auth/reset-password', {
      method: 'POST',
      headers: wizardHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ server_url: getServerUrl(), email: email, name: name, new_password: newPwd }),
    });
    var data = await resp.json();
    if (resp.ok && data.employee_id) {
      authUser = { employee_id: data.employee_id, name: data.name || '', department: data.department || '', auth_token: data.auth_token || '' };
      showMsg('rpMsg', '密码重置成功！', 'success');
      setTimeout(function(){ goToStep2(); }, 800);
    } else {
      var msg = data.detail || data.message || '重置失败';
      if (resp.status === 404) msg = '未找到该邮箱对应的账号';
      if (resp.status === 403) msg = '姓名与该账号记录不匹配';
      showMsg('rpMsg', msg, 'error');
    }
  } catch(err) {
    showMsg('rpMsg', '网络错误: ' + err.message, 'error');
  }
  btn.disabled = false; btn.textContent = '重置密码';
}

function goToStep2() {
  document.getElementById('step1').classList.remove('active');
  document.getElementById('step2').classList.add('active');
  document.getElementById('dot1').classList.remove('active');
  document.getElementById('dot2').classList.add('active');
  setSetupVisible(!!authUser);
  if (authUser) {
    document.getElementById('displayName').textContent = authUser.name + '（' + authUser.employee_id + '）';
    var detail = '';
    if (authUser.department) detail += '部门：' + authUser.department + '  ';
    detail += '服务器：' + getServerUrl();
    document.getElementById('displayDetail').textContent = detail;
  }
}

function setSetupVisible(visible) {
  var setupActions = document.getElementById('setupActions');
  if (setupActions) setupActions.style.display = visible ? 'block' : 'none';
}

function showAuthStep(tab) {
  document.getElementById('step2').classList.remove('active');
  document.getElementById('step1').classList.add('active');
  document.getElementById('dot2').classList.remove('active');
  document.getElementById('dot1').classList.add('active');
  document.getElementById('status').className = '';
  document.getElementById('status').style.display = 'none';
  switchTab(tab || 'login');
}

function toggleAdvanced() {
  document.getElementById('advancedSection').classList.toggle('show');
}

function waitForMonitorAndRedirect(nextURL, statusEl) {
  // 拆出 origin 探活（/status 是无认证 JSON 健康端点），200 即认为代理就绪。
  var origin = nextURL.replace(/\/wizard.*$/, '');
  var probeURL = origin + '/status';
  var deadline = Date.now() + 60000;
  var attempts = 0;
  function tick() {
    attempts++;
    fetch(probeURL, { cache: 'no-store' }).then(function(r) {
      if (r.ok) {
        if (statusEl) statusEl.innerHTML = '✓ 监控服务已就绪，正在跳转...';
        setTimeout(function() { window.location.replace(nextURL); }, 400);
        return;
      }
      throw new Error('status ' + r.status);
    }).catch(function() {
      if (Date.now() > deadline) {
        if (statusEl) {
          statusEl.className = 'error';
          statusEl.innerHTML = '✗ 监控服务未在 60 秒内就绪。<br>请手动打开：<a href="' + nextURL + '" style="color:#38bdf8;">' + nextURL + '</a>';
        }
        return;
      }
      setTimeout(tick, attempts < 5 ? 600 : 1200);
    });
  }
  tick();
}

async function doInstall() {
  if (!authUser) { alert('请先登录或注册'); showAuthStep('login'); return; }
  var btn = document.getElementById('installBtn');
  var status = document.getElementById('status');
  btn.disabled = true; btn.textContent = '正在安装...';
  status.className = 'info'; status.style.display = 'block';
  status.textContent = '正在保存配置并安装证书...';

  try {
    var resp = await fetch(basePath + '/api/setup', {
      method: 'POST',
      headers: wizardHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({
        user_name: authUser.name,
        user_id: authUser.employee_id,
        department: authUser.department,
        server_url: getServerUrl(),
        upstream_proxy: document.getElementById('upstreamProxy').value.trim(),
        port: parseInt(document.getElementById('port').value) || 18090,
        auth_token: authUser.auth_token,
      }),
    });
    var result = await resp.json();
    if (result.success) {
      status.className = 'success';
      btn.textContent = '✓ 已完成';
      if (result.next_url) {
        // 立即给一个可点链接做兜底——即使 60s 轮询失败，用户仍可手动跳过去。
        status.innerHTML = '✓ 安装成功，正在启动监控服务...<br>'
          + '<span style="opacity:.7;">如未自动跳转，请点击：</span> '
          + '<a href="' + result.next_url + '" style="color:#38bdf8;">' + result.next_url + '</a>';
        waitForMonitorAndRedirect(result.next_url, status);
      } else {
        status.innerHTML = '✓ 安装成功！<br><br>' + result.message.replace(/\n/g, '<br>') + '<br><br>此页面可以关闭了。';
      }
    } else {
      status.className = 'error';
      status.textContent = '✗ ' + result.message;
      btn.disabled = false; btn.textContent = '重试安装';
    }
  } catch(err) {
    status.className = 'error';
    status.textContent = '✗ 网络错误: ' + err.message;
    btn.disabled = false; btn.textContent = '重试安装';
  }
}

window.addEventListener('load', function() {
  refreshConsoleStatus().then(function(supported) {
    if (supported) {
      // 运行态默认进入控制台页；已登录后不再展示注册/登录入口。
      goToStep2();
      loadOverview();
      setInterval(loadOverview, 30000);
    }
  });
  initLogPanel();
});

// ── 使用统计 ─────────────────────────────────────────────────
var overviewState = { days: 1, loading: false };

function setRange(days) {
  overviewState.days = days;
  ['1','7','15','30'].forEach(function(d) {
    var btn = document.getElementById('rangeBtn' + d);
    if (btn) btn.classList.toggle('active', String(days) === d);
  });
  var label = days === 1 ? '今日' : ('近' + days + '天');
  ['Tokens', 'Requests', 'Cost'].forEach(function(k, i) {
    var el = document.getElementById('stat' + k + 'Label');
    var names = ['Tokens', '请求', '成本'];
    if (el) el.textContent = label + ' ' + names[i];
  });
  var usersLabel = document.getElementById('statUsersLabel');
  if (usersLabel) usersLabel.textContent = label + ' 缓存命中率';
  loadOverview();
}

async function loadOverview() {
  if (overviewState.loading) return;
  overviewState.loading = true;
  try {
    var resp = await fetch(basePath + '/api/overview?days=' + overviewState.days);
    if (!resp.ok) return;
    var data = await resp.json();
    if (!data || typeof data !== 'object') return;
    var tokens = Number(data.total_tokens || 0);
    var requests = Number(data.total_requests || 0);
    var cost = Number(data.total_cost_cny || 0);
    document.getElementById('statTokens').textContent = tokens.toLocaleString();
    document.getElementById('statRequests').textContent = requests.toLocaleString();
    document.getElementById('statCost').textContent = '¥' + cost.toFixed(2);
    var cacheRead = Number(data.cache_read_tokens || 0);
    var cacheCreate = Number(data.cache_creation_tokens || 0);
    var inputTokens = Number(data.input_tokens || 0);
    // 命中率 = cache_read / (cache_read + 已计费 input)。input_tokens 已含折算后的 cache 量，作为近似分母。
    var denom = cacheRead + inputTokens;
    var rateEl = document.getElementById('statUsers');
    var subEl = document.getElementById('statUsersSub');
    if (cacheRead + cacheCreate > 0 && denom > 0) {
      var rate = cacheRead / denom * 100;
      rateEl.textContent = rate.toFixed(1) + '%';
      subEl.textContent = '读 ' + cacheRead.toLocaleString() + ' / 写 ' + cacheCreate.toLocaleString();
    } else {
      rateEl.textContent = '—';
      subEl.textContent = '暂无命中';
    }
  } catch (err) {
    // 网络/服务器不可达时静默；下一次轮询自然重试
  } finally {
    overviewState.loading = false;
  }
}

// ── 运行日志面板 ──────────────────────────────────────────────
// 从 /wizard/api/logs/history 拉历史，再走 EventSource 订阅实时流。
// 控制台里看到的内容（[MITM]/[记录]/[上报]/banner 等）和这里完全一致。
var logState = { lastSeq: 0, evt: null, paused: false };

function escHtml(s) { return s.replace(/[&<>"']/g, function(c) { return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c]; }); }

// renderLogLine returns innerHTML for a single log line:
//   * 剥掉 Go log 包前缀的 "YYYY/MM/DD HH:MM:SS.uuuuuu " 时间戳，避免和我们的短时间重复
//   * 识别 [tag] 染成胶囊
//   * 数字 / model / sourceApp / HTTP 状态码 高亮
function renderLogLine(text) {
  text = text.replace(/^\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?\s+/, '');
  var head = '';
  var m = text.match(/^\[([^\]]+)\]\s*/);
  if (m) {
    var tag = m[1];
    head = '<span class="tag" data-tag="' + escHtml(tag) + '">' + escHtml(tag) + '</span>';
    text = text.slice(m[0].length);
  }
  var body = escHtml(text);
  body = body.replace(/\bmodel=&quot;([^&]+?)&quot;/g, 'model=<span class="model">"$1"</span>');
  body = body.replace(/\bsourceApp=&quot;([^&]+?)&quot;/g, 'sourceApp=<span class="app">"$1"</span>');
  body = body.replace(/\b(prompt|completion|total|tokens)=(\d+)/g, '<span class="k">$1</span>=<span class="num">$2</span>');
  body = body.replace(/(输入|输出|总计|累计)\s*[:：]\s*(\d+)/g, '<span class="k">$1</span>:<span class="num">$2</span>');
  body = body.replace(/(→\s*)(\d{3})\b/g, function(_, arrow, code) {
    var c = code.charAt(0);
    var cls = c==='2'?'st-2':c==='4'?'st-4':c==='5'?'st-5':'st-3';
    return arrow + '<span class="' + cls + '">' + code + '</span>';
  });
  body = body.replace(/(https?:\/\/[^\s]+)/g, '<span class="url">$1</span>');
  return head + body;
}

function appendLogLine(ln) {
  var body = document.getElementById('logBody');
  if (!body) return;
  if (ln.seq <= logState.lastSeq) return;
  logState.lastSeq = ln.seq;
  var div = document.createElement('div');
  if (/\b(error|失败|ERROR|FAIL)\b/.test(ln.text)) div.className = 'ln-err';
  div.innerHTML = '<span class="ts">' + escHtml(ln.time) + '</span>' + renderLogLine(ln.text);
  body.appendChild(div);
  while (body.childNodes.length > 2000) body.removeChild(body.firstChild);
  body.scrollTop = body.scrollHeight;
}

function setLogLive(live) {
  var lbl = document.getElementById('logLbl');
  if (!lbl) return;
  if (live) lbl.classList.add('live'); else lbl.classList.remove('live');
}

function initLogPanel() {
  if (!window.EventSource) {
    var b = document.getElementById('logBody');
    if (b) b.textContent = '当前 WebView 不支持 EventSource，无法显示日志。';
    return;
  }
  fetch(basePath + '/api/logs/history').then(function(r) { return r.json(); }).then(function(arr) {
    (arr || []).forEach(appendLogLine);
  }).catch(function() {}).finally(function() {
    try {
      logState.evt = new EventSource(basePath + '/api/logs/stream');
      logState.evt.onopen = function() { setLogLive(true); };
      logState.evt.onerror = function() { setLogLive(false); }; // 浏览器会自动重连
      logState.evt.onmessage = function(e) {
        try { appendLogLine(JSON.parse(e.data)); } catch (err) {}
      };
    } catch (err) {}
  });
}
</script>
</body>
</html>`

type setupRequest struct {
	UserName      string `json:"user_name"`
	UserID        string `json:"user_id"`
	Department    string `json:"department"`
	ServerURL     string `json:"server_url"`
	UpstreamProxy string `json:"upstream_proxy"`
	Port          int    `json:"port"`
	AuthToken     string `json:"auth_token"`
}

type setupResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NextURL string `json:"next_url,omitempty"`
}

type consoleStatusResponse struct {
	Success       bool   `json:"success"`
	Supported     bool   `json:"supported"`
	Mode          string `json:"mode,omitempty"`
	UserName      string `json:"user_name,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	Department    string `json:"department,omitempty"`
	ServerURL     string `json:"server_url,omitempty"`
	UpstreamProxy string `json:"upstream_proxy,omitempty"`
	TotalReported int64  `json:"total_reported,omitempty"`
	Message       string `json:"message,omitempty"`
}

type consoleModeRequest struct {
	Mode string `json:"mode"`
}

type consoleLaunchRequest struct {
	Preset         string   `json:"preset"`
	CustomBinary   string   `json:"custom_binary"`
	CustomArgs     []string `json:"custom_args"`
	RestartRunning bool     `json:"restart_running"`
}

type consoleActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

var wizardActionMu sync.Mutex

const wizardTokenHeader = "X-AI-Monitor-Wizard-Token"

func (s *ProxyServer) authorizeWizardAction(r *http.Request) bool {
	if s == nil || strings.TrimSpace(s.wizardToken) == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.Header.Get(wizardTokenHeader)), []byte(s.wizardToken)) == 1
}

func (s *ProxyServer) rejectUnauthorizedWizardAction(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "wizard token 无效"})
}

// runWebWizard starts a local web server with a setup page and opens it in the browser.
// Blocks until the user completes setup or closes the page.
func runWebWizard(configPath string, certMgr *CertManager) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("无法启动本地 Web 服务: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	wizardURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	done := make(chan struct{})
	var setupErr error

	mux := http.NewServeMux()

	// Serve the setup page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.New("wizard").Parse(webWizardHTML)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		data := struct {
			UserName     string
			ServerURL    string
			BasePath     string
			WizardToken  string
			FirstInstall bool
		}{
			UserName:     getOSUserName(),
			ServerURL:    DefaultServerURL,
			BasePath:     "",
			WizardToken:  "",
			FirstInstall: true,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("[wizard] template execute failed: %v", err)
		}
	})

	// Proxy auth requests to the remote server (register / login)
	mux.HandleFunc("/api/auth/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}

		var reqBody struct {
			ServerURL string `json:"server_url"`
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()
		json.Unmarshal(bodyBytes, &reqBody)

		serverURL := strings.TrimRight(strings.TrimSpace(reqBody.ServerURL), "/")
		if serverURL == "" {
			serverURL = DefaultServerURL
		}

		authPath := strings.TrimPrefix(r.URL.Path, "/api/auth/")
		targetURL := serverURL + "/api/auth/" + authPath

		client := &http.Client{Timeout: 15 * time.Second}
		proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			json.NewEncoder(w).Encode(map[string]string{"detail": "请求构建失败: " + err.Error()})
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(proxyReq)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			json.NewEncoder(w).Encode(map[string]string{"detail": "无法连接服务器: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	})

	mux.HandleFunc("/api/console/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(consoleStatusResponse{
			Success:   true,
			Supported: false,
			Message:   "当前为首次安装向导，尚未进入运行态控制台",
		})
	})
	mux.HandleFunc("/api/console/mode", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(consoleActionResponse{
			Success: false,
			Message: "首次安装向导不支持切换运行模式，请先完成安装并正常启动 ai-monitor",
		})
	})
	mux.HandleFunc("/api/console/launch", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(consoleActionResponse{
			Success: false,
			Message: "首次安装向导不支持一键启动，请先完成安装并正常启动 ai-monitor",
		})
	})

	// Handle setup submission
	mux.HandleFunc("/api/setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}

		var req setupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(setupResponse{Success: false, Message: "请求格式错误: " + err.Error()})
			return
		}

		if strings.TrimSpace(req.UserName) == "" {
			json.NewEncoder(w).Encode(setupResponse{Success: false, Message: "姓名不能为空"})
			return
		}

		// Build config
		cfg := &Config{
			ServerURL:     strings.TrimRight(strings.TrimSpace(req.ServerURL), "/"),
			UserName:      strings.TrimSpace(req.UserName),
			UserID:        strings.TrimSpace(req.UserID),
			Department:    strings.TrimSpace(req.Department),
			Port:          req.Port,
			UpstreamProxy: strings.TrimSpace(req.UpstreamProxy),
			AuthToken:     strings.TrimSpace(req.AuthToken),
		}
		if cfg.ServerURL == "" {
			cfg.ServerURL = DefaultServerURL
		}
		if cfg.UserID == "" {
			cfg.UserID = generateUserID()
		}
		if cfg.Port <= 0 || cfg.Port > 65535 {
			cfg.Port = 18090
		}

		if err := validateServerURL(cfg.ServerURL); err != nil {
			json.NewEncoder(w).Encode(setupResponse{Success: false, Message: "服务器地址无效: " + err.Error()})
			return
		}
		if cfg.UpstreamProxy != "" {
			if err := validateUpstreamProxyURL(cfg.UpstreamProxy); err != nil {
				json.NewEncoder(w).Encode(setupResponse{Success: false, Message: "上游代理地址无效: " + err.Error()})
				return
			}
		}

		// Save the single annotated config.json used by both ai-monitor and the VS Code extension.
		absConfigPath, _ := filepath.Abs(configPath)
		if err := SaveConfig(cfg, absConfigPath); err != nil {
			json.NewEncoder(w).Encode(setupResponse{Success: false, Message: "保存配置失败: " + err.Error()})
			return
		}
		_ = os.Remove(filepath.Join(appDataDir(), "identity.json"))

		var messages []string

		// 1. Install CA
		if err := certMgr.InstallCA(); err != nil {
			messages = append(messages, "⚠ CA 证书安装失败: "+err.Error())
		} else {
			messages = append(messages, "✓ CA 证书已安装")
		}

		messages = append(messages, "✓ 已保存低侵入配置")
		messages = append(messages, "✓ 未写入系统代理 / 用户环境变量 / IDE settings / 开机自启")

		messages = append(messages, "")
		messages = append(messages, "后续通过 低侵入监控.bat 或 启动-VSCode监控.bat / 启动-Cursor监控.bat 打开监控。")

		w.Header().Set("Content-Type", "application/json")
		nextPort := req.Port
		if nextPort <= 0 || nextPort > 65535 {
			nextPort = 18090
		}
		// 关键：如果机器上已有一个 ai-monitor 在跑（比如旧版未关），新进程会因为
		// 单例检查直接 os.Exit，新配置永远不会生效。这里主动把它干掉，让 main.go
		// 在 runWebWizard 返回后能顺利 startMonitorRuntime 起到 req.Port 上。
		if existingPort, alive := checkExistingInstance(); alive {
			log.Printf("[wizard] 检测到旧实例占用 :%d，正在终止以便新配置生效", existingPort)
			stopExistingInstanceForUninstall()
			// 给被 kill 的进程让一点点时间释放端口
			time.Sleep(800 * time.Millisecond)
		}
		json.NewEncoder(w).Encode(setupResponse{
			Success: true,
			Message: strings.Join(messages, "\n"),
			NextURL: fmt.Sprintf("http://127.0.0.1:%d/wizard", nextPort),
		})

		// Signal completion after a short delay (let response send)
		go func() {
			time.Sleep(500 * time.Millisecond)
			close(done)
		}()
	})

	server := &http.Server{Handler: mux}

	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			setupErr = err
			close(done)
		}
	}()

	log.Printf("[wizard] 安装向导已启动: %s", wizardURL)
	windowDone, closeWindow := openWizardOrBrowser(wizardURL, "AI Token 监控")

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════╗")
	if windowDone != nil {
		fmt.Println("  ║   安装向导已在内嵌窗口中打开              ║")
	} else {
		fmt.Println("  ║   安装向导已在浏览器中打开                ║")
	}
	fmt.Printf("  ║   %s            ║\n", wizardURL)
	fmt.Println("  ║   请完成配置                              ║")
	fmt.Println("  ╚══════════════════════════════════════════╝")
	fmt.Println()

	// Wait for: setup completion, user-closed window, or 10-minute timeout.
	if windowDone == nil {
		windowDone = make(chan struct{}) // never closes; only done/timeout matters
	}
	select {
	case <-done:
		// Setup completed (or HTTP server errored).
		// 成功路径也关闭首次安装窗口，后续由运行态重新打开控制台窗口。
		// 这样可以避免旧向导窗口停留在已关闭的临时端口页面。
		if closeWindow != nil {
			closeWindow()
			select {
			case <-windowDone:
			case <-time.After(3 * time.Second):
				log.Println("[wizard] 安装向导窗口未在 3s 内关闭，继续后续流程")
			}
		}
	case <-windowDone:
		// User closed the wizard window before completing setup. Treat as abort.
		if setupErr == nil {
			setupErr = errors.New("用户关闭了安装向导窗口")
		}
	case <-time.After(10 * time.Minute):
		log.Println("[wizard] 超时，关闭向导")
		if closeWindow != nil {
			closeWindow()
		}
		select {
		case <-windowDone:
		case <-time.After(3 * time.Second):
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	return setupErr
}

// serveWizard handles wizard requests on the running MITM proxy (path: /wizard/*).
// Unlike runWebWizard which runs during initial setup, this is always available while monitoring.
func (s *ProxyServer) serveWizard(w http.ResponseWriter, r *http.Request) {
	// Strip /wizard prefix to get the sub-path
	subPath := strings.TrimPrefix(r.URL.Path, "/wizard")
	if subPath == "" || subPath == "/" {
		// Serve the wizard HTML page
		tmpl, err := template.New("wizard").Parse(webWizardHTML)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		data := struct {
			UserName     string
			UserID       string
			ServerURL    string
			BasePath     string
			WizardToken  string
			FirstInstall bool
		}{
			UserName:     s.cfg.UserName,
			UserID:       s.cfg.UserID,
			ServerURL:    s.cfg.ServerURL,
			BasePath:     "/wizard",
			WizardToken:  s.wizardToken,
			FirstInstall: false,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, data)
		return
	}

	if subPath == "/api/console/status" && r.Method == http.MethodGet {
		s.handleConsoleStatus(w, r)
		return
	}
	if subPath == "/api/console/mode" && r.Method == http.MethodPost {
		if !s.authorizeWizardAction(r) {
			s.rejectUnauthorizedWizardAction(w)
			return
		}
		s.handleConsoleModeSwitch(w, r)
		return
	}
	if subPath == "/api/console/launch" && r.Method == http.MethodPost {
		if !s.authorizeWizardAction(r) {
			s.rejectUnauthorizedWizardAction(w)
			return
		}
		s.handleConsoleLaunch(w, r)
		return
	}
	if subPath == "/api/logs/history" && r.Method == http.MethodGet {
		handleLogsHistory(w, r)
		return
	}
	if subPath == "/api/logs/stream" && r.Method == http.MethodGet {
		handleLogsStream(w, r)
		return
	}
	if subPath == "/api/overview" && r.Method == http.MethodGet {
		s.handleOverviewProxy(w, r)
		return
	}

	// Proxy auth requests to the remote server
	if strings.HasPrefix(subPath, "/api/auth/") && r.Method == http.MethodPost {
		if !s.authorizeWizardAction(r) {
			s.rejectUnauthorizedWizardAction(w)
			return
		}
		var reqBody struct {
			ServerURL string `json:"server_url"`
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()
		json.Unmarshal(bodyBytes, &reqBody)

		serverURL := strings.TrimRight(strings.TrimSpace(reqBody.ServerURL), "/")
		if serverURL == "" {
			serverURL = s.cfg.ServerURL
		}

		authPath := strings.TrimPrefix(subPath, "/api/auth/")
		targetURL := serverURL + "/api/auth/" + authPath

		client := &http.Client{Timeout: 15 * time.Second}
		proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			json.NewEncoder(w).Encode(map[string]string{"detail": "请求构建失败: " + err.Error()})
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(proxyReq)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			json.NewEncoder(w).Encode(map[string]string{"detail": "无法连接服务器: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Handle setup submission (runtime mode: only update config, no install steps)
	if subPath == "/api/setup" && r.Method == http.MethodPost {
		if !s.authorizeWizardAction(r) {
			s.rejectUnauthorizedWizardAction(w)
			return
		}
		var req setupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(setupResponse{Success: false, Message: "请求格式错误: " + err.Error()})
			return
		}

		// Update config fields
		if name := strings.TrimSpace(req.UserName); name != "" {
			s.cfg.UserName = name
		}
		if uid := strings.TrimSpace(req.UserID); uid != "" {
			s.cfg.UserID = uid
		}
		if dept := strings.TrimSpace(req.Department); dept != "" {
			s.cfg.Department = dept
		}
		if surl := strings.TrimRight(strings.TrimSpace(req.ServerURL), "/"); surl != "" {
			s.cfg.ServerURL = surl
		}
		if proxy := strings.TrimSpace(req.UpstreamProxy); proxy != "" {
			s.cfg.UpstreamProxy = proxy
		}
		if token := strings.TrimSpace(req.AuthToken); token != "" {
			s.cfg.AuthToken = token
		}
		if req.Port > 0 && req.Port <= 65535 {
			s.cfg.Port = req.Port
		}

		// Save the single annotated config.json.
		absConfigPath, _ := filepath.Abs(s.configPath)
		if err := SaveConfig(s.cfg, absConfigPath); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(setupResponse{Success: false, Message: "保存配置失败: " + err.Error()})
			return
		}

		log.Printf("[wizard] 配置已更新: %s", absConfigPath)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(setupResponse{
			Success: true,
			Message: "✓ 配置已更新并保存到 " + absConfigPath + "\n\n部分配置（如端口、上游代理）需重启 ai-monitor 后生效。",
		})
		return
	}

	http.NotFound(w, r)
}

func (s *ProxyServer) handleOverviewProxy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	serverURL := strings.TrimRight(strings.TrimSpace(s.cfg.ServerURL), "/")
	if serverURL == "" {
		w.WriteHeader(400)
		w.Write([]byte(`{"detail":"server_url 未配置"}`))
		return
	}
	days := strings.TrimSpace(r.URL.Query().Get("days"))
	if days == "" {
		days = "1"
	}
	target := serverURL + "/api/dashboard/overview?days=" + url.QueryEscape(days)
	if eid := strings.TrimSpace(s.cfg.UserID); eid != "" {
		target += "&employee_id=" + url.QueryEscape(eid)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		w.WriteHeader(502)
		w.Write([]byte(`{"detail":"上游不可达: ` + err.Error() + `"}`))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (s *ProxyServer) handleConsoleStatus(w http.ResponseWriter, _ *http.Request) {
	resp := consoleStatusResponse{
		Success:    true,
		Supported:  true,
		Mode:       s.currentTakeoverMode(),
		UserName:   s.cfg.UserName,
		UserID:     s.cfg.UserID,
		Department: s.cfg.Department,
		ServerURL:  s.cfg.ServerURL,
		UpstreamProxy: func() string {
			if strings.TrimSpace(s.cfg.UpstreamProxy) == "" {
				return "(direct)"
			}
			return strings.TrimSpace(s.cfg.UpstreamProxy)
		}(),
	}
	if s.reporter != nil {
		resp.TotalReported = s.reporter.Stats.TotalReported.Load()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *ProxyServer) handleConsoleModeSwitch(w http.ResponseWriter, r *http.Request) {
	var req consoleModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "请求格式错误"})
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))

	// 锁尝试 100ms：如果有切换在跑，直接告诉用户「忙」，不要让 HTTP 请求挂着排队，
	// 否则前端连续点几下会全部卡在 Mutex 上，看起来像「点了没反应」。
	if !tryLockWithTimeout(&wizardActionMu, 100*time.Millisecond) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "上一次切换还在进行，请稍候再试"})
		return
	}
	defer wizardActionMu.Unlock()

	overallStart := time.Now()
	log.Printf("[mode-switch] ▶ 开始切换：%s", mode)
	defer func() {
		log.Printf("[mode-switch] ◀ 结束切换：%s 共耗时 %s", mode, time.Since(overallStart).Round(time.Millisecond))
	}()

	step := func(name string, fn func()) {
		t0 := time.Now()
		fn()
		log.Printf("[mode-switch]   · %s 耗时 %s", name, time.Since(t0).Round(time.Millisecond))
	}

	var message string
	switch mode {
	case "observe":
		var st *InstallState
		step("loadInstallState", func() { st = loadInstallState() })
		step("restoreProxyFromState", func() { restoreProxyFromState(st) })
		step("restoreOrClearEnvVars", func() { restoreOrClearEnvVars(st) })
		if st != nil && st.SessionOnly {
			step("clearInstallState", func() { clearInstallState() })
		}
		s.SetTakeoverMode("observe")
		message = "已切换到观察模式（当前不接管系统网络）"
	case "session_pac":
		st := loadInstallState()
		if st != nil && st.SystemProxySet && !st.SessionOnly {
			step("applySessionManagedProxy", func() { applySessionManagedProxy(s.cfg, s.certMgr, s.listenPort) })
			s.SetTakeoverMode("persistent")
			message = "当前为全局接管配置，已保持持久接管模式生效"
		} else {
			step("applyTemporarySessionProxy", func() { applyTemporarySessionProxy(s.cfg, s.certMgr, s.listenPort) })
			s.SetTakeoverMode("session")
			message = "已切换到会话PAC模式（退出自动还原）"
		}
	case "global_install":
		step("doGlobalInstall", func() { doGlobalInstall(s.certMgr, s.cfg, s.configPath) })
		step("applySessionManagedProxy", func() { applySessionManagedProxy(s.cfg, s.certMgr, s.listenPort) })
		s.SetTakeoverMode("persistent")
		message = "已切换到全局接管模式（持久）"
	case "cleanup":
		var st *InstallState
		step("loadInstallState", func() { st = loadInstallState() })
		step("restoreProxyFromState", func() { restoreProxyFromState(st) })
		step("clearAIMonitorPACIfCurrent", func() { clearAIMonitorPACIfCurrent() })
		step("restoreOrClearEnvVars", func() { restoreOrClearEnvVars(st) })
		step("removeAIMonitorIDEProxy", func() { removeAIMonitorIDEProxy() })
		if clearConfiguredUpstreamProxy(s.configPath) {
			s.cfg.UpstreamProxy = ""
		}
		step("clearSavedUpstreamProxy", func() { clearSavedUpstreamProxy() })
		step("clearInstallState", func() { clearInstallState() })
		s.SetTakeoverMode("observe")
		message = "网络接管残留已清理，当前为观察模式"
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "未知模式"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: true, Message: message})
}

// tryLockWithTimeout 尝试在 timeout 内获取 mu；成功返回 true。
// sync.Mutex 自身没有 TryLock+timeout 组合，这里用轮询模拟，开销可以忽略（1ms 一次）。
func tryLockWithTimeout(mu *sync.Mutex, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if mu.TryLock() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func validateConsoleLaunchRequest(req consoleLaunchRequest) consoleActionResponse {
	if strings.TrimSpace(req.CustomBinary) != "" || len(req.CustomArgs) > 0 {
		return consoleActionResponse{Success: false, Message: "仅允许使用 preset 启动内置应用"}
	}
	if strings.TrimSpace(req.Preset) == "" {
		return consoleActionResponse{Success: false, Message: "请提供 preset"}
	}
	return consoleActionResponse{Success: true}
}

func (s *ProxyServer) handleConsoleLaunch(w http.ResponseWriter, r *http.Request) {
	var req consoleLaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "请求格式错误"})
		return
	}
	if validation := validateConsoleLaunchRequest(req); !validation.Success {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(validation)
		return
	}

	wizardActionMu.Lock()
	defer wizardActionMu.Unlock()

	presetName := strings.ToLower(strings.TrimSpace(req.Preset))
	command, preset, err := resolveLaunchCommand(nil, presetName, exec.LookPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: err.Error()})
		return
	}
	if req.RestartRunning {
		if image, _ := managedPresetProcessImage(preset); image != "" {
			_ = newHiddenCmd("taskkill", "/F", "/IM", image).Run()
			time.Sleep(1200 * time.Millisecond)
		}
	}

	if err := launchChildWithExistingProxyDetached(s.cfg, s.certMgr, command, preset, s.listenPort); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "启动失败: " + err.Error()})
		return
	}

	target := presetName
	if target == "" {
		target = command[0]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(consoleActionResponse{
		Success: true,
		Message: "已启动 " + target + "（自动注入代理环境）",
	})
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = newHiddenCmd("cmd", "/C", "start", url)
	case "darwin":
		cmd = newHiddenCmd("open", url)
	default:
		cmd = newHiddenCmd("xdg-open", url)
	}
	cmd.Start()
}

// openWizardOrBrowser tries to open the wizard in an embedded WebView2
// window first; on failure (WebView2 runtime missing, non-Windows, etc.)
// it falls back to the system browser. Returns the window's close channel
// (nil when fallback was used) and the captured close callback (nil when
// fallback was used). Fire-and-forget callers can ignore both returns.
func openWizardOrBrowser(url, title string) (<-chan struct{}, func()) {
	var closer func()
	done, err := openWizardWindow(url, title, &closer)
	if err != nil {
		log.Printf("[wizard] 内嵌窗口不可用 (%v)，回退到系统浏览器", err)
		openBrowser(url)
		return nil, nil
	}
	return done, closer
}
