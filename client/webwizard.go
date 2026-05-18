package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
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
<title>AI Token 监控 - 安装向导</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif; background: #0f172a; color: #e2e8f0; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
  .card { background: #1e293b; border-radius: 16px; box-shadow: 0 25px 50px rgba(0,0,0,0.5); padding: 40px; width: 860px; max-width: 96vw; }
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
  #status { margin-top: 20px; padding: 12px; border-radius: 8px; font-size: 13px; display: none; line-height: 1.6; }
  #status.success { display: block; background: #064e3b; color: #6ee7b7; border: 1px solid #065f46; }
  #status.error { display: block; background: #450a0a; color: #fca5a5; border: 1px solid #7f1d1d; }
  #status.info { display: block; background: #1e3a5f; color: #93c5fd; border: 1px solid #1e40af; }
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
    <div class="status-bar mono" id="quickStatus">模式: 未知 | 上游: (direct) | 已上报: 0</div>

    <div class="panel">
      <div class="panel-title">一键模式切换</div>
      <div class="btn-grid">
        <button class="btn-secondary btn-small" id="modeObserveBtn" onclick="switchMode('observe')">观察模式</button>
        <button class="btn-secondary btn-small" id="modeSessionBtn" onclick="switchMode('session_pac')">会话PAC模式</button>
        <button class="btn-secondary btn-small" id="modeGlobalBtn" onclick="switchMode('global_install')">全局接管模式</button>
        <button class="btn-secondary btn-small" id="modeCleanupBtn" onclick="switchMode('cleanup')">一键恢复网络</button>
      </div>
      <div class="hint" id="modeHint" style="margin-top:8px;">当前模式: 未获取</div>
    </div>

    <div class="panel">
      <div class="panel-title">一键启动编辑器 / 终端（已启用自动重启同名运行实例）</div>
      <div class="btn-grid">
        <button class="btn-secondary btn-small" onclick="launchPreset('cursor')">启动 Cursor</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('vscode')">启动 VS Code</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('powershell')">启动 PowerShell</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('cmd')">启动 CMD</button>
      </div>
      <div class="hint" style="margin-top:8px;">更多 IDE（含 JetBrains）</div>
      <div class="btn-grid-3">
        <button class="btn-secondary btn-small" onclick="launchPreset('windsurf')">Windsurf</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('kiro')">Kiro</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('trae')">Trae</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('idea')">IntelliJ IDEA</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('webstorm')">WebStorm</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('pycharm')">PyCharm</button>
        <button class="btn-secondary btn-small" onclick="launchPreset('goland')">GoLand</button>
      </div>
      <div class="field" style="margin-top:10px;">
        <label>自定义程序路径</label>
        <input id="customBinary" type="text" placeholder="例如：C:\Program Files\Git\bin\bash.exe" />
      </div>
      <div class="field">
        <label>参数（可选）</label>
        <input id="customArgs" type="text" placeholder='例如：-l -i' />
      </div>
      <button class="btn-secondary btn-small" onclick="launchCustom()">启动自定义程序</button>
    </div>

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

<script>
var authUser = null;
var basePath = '{{.BasePath}}';
var consoleStatus = null;

function setStatus(level, text) {
  var status = document.getElementById('status');
  status.className = level;
  status.style.display = 'block';
  status.textContent = text;
}

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
    document.getElementById('quickStatus').textContent = '模式: ' + shortMode(data.mode) + ' | 上游: ' + upstream + ' | 已上报: ' + (data.total_reported || 0);
    return true;
  } catch (err) {
    return false;
  }
  return false;
}

async function switchMode(mode) {
  var label = modeText(mode);
  if (mode === 'global_install' && !confirm('确认切换到全局接管模式？这会写入持久网络设置。')) return;
  if (mode === 'cleanup' && !confirm('确认执行一键恢复网络？这会清理当前代理接管设置。')) return;
  setStatus('info', '正在切换模式：' + label + ' ...');
  try {
    var resp = await fetch(basePath + '/api/console/mode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: mode }),
    });
    var data = await resp.json();
    if (resp.ok && data.success) {
      setStatus('success', '✓ ' + data.message);
      await refreshConsoleStatus();
    } else {
      setStatus('error', '✗ ' + (data.message || '模式切换失败'));
    }
  } catch (err) {
    setStatus('error', '✗ 网络错误: ' + err.message);
  }
}

async function launchPreset(name) {
  setStatus('info', '正在启动：' + name + ' ...');
  try {
    var resp = await fetch(basePath + '/api/console/launch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
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

async function launchCustom() {
  var binary = document.getElementById('customBinary').value.trim();
  var argsText = document.getElementById('customArgs').value.trim();
  if (!binary) {
    setStatus('error', '✗ 请先输入自定义程序路径');
    return;
  }
  var args = argsText ? argsText.split(/\s+/).filter(Boolean) : [];
  setStatus('info', '正在启动自定义程序...');
  try {
    var resp = await fetch(basePath + '/api/console/launch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ custom_binary: binary, custom_args: args, restart_running: true }),
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
      headers: { 'Content-Type': 'application/json' },
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
      headers: { 'Content-Type': 'application/json' },
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
      headers: { 'Content-Type': 'application/json' },
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
      headers: { 'Content-Type': 'application/json' },
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
      status.innerHTML = '✓ 安装成功！<br><br>' + result.message.replace(/\n/g, '<br>') + '<br><br>此页面可以关闭了。';
      btn.textContent = '✓ 已完成';
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
    }
  });
});
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
			UserName  string
			ServerURL string
			BasePath  string
		}{
			UserName:  getOSUserName(),
			ServerURL: DefaultServerURL,
			BasePath:  "",
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, data)
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
		json.NewEncoder(w).Encode(setupResponse{
			Success: true,
			Message: strings.Join(messages, "\n"),
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
	openBrowser(wizardURL)

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════╗")
	fmt.Println("  ║   安装向导已在浏览器中打开                ║")
	fmt.Printf("  ║   %s            ║\n", wizardURL)
	fmt.Println("  ║   请在浏览器中完成配置                    ║")
	fmt.Println("  ╚══════════════════════════════════════════╝")
	fmt.Println()

	// Wait for setup completion or timeout (10 minutes)
	select {
	case <-done:
	case <-time.After(10 * time.Minute):
		log.Println("[wizard] 超时，关闭向导")
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
			UserName  string
			ServerURL string
			BasePath  string
		}{
			UserName:  s.cfg.UserName,
			ServerURL: s.cfg.ServerURL,
			BasePath:  "/wizard",
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
		s.handleConsoleModeSwitch(w, r)
		return
	}
	if subPath == "/api/console/launch" && r.Method == http.MethodPost {
		s.handleConsoleLaunch(w, r)
		return
	}

	// Proxy auth requests to the remote server
	if strings.HasPrefix(subPath, "/api/auth/") && r.Method == http.MethodPost {
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
	wizardActionMu.Lock()
	defer wizardActionMu.Unlock()

	var message string
	switch mode {
	case "observe":
		st := loadInstallState()
		restoreProxyFromState(st)
		restoreOrClearEnvVars(st)
		if st != nil && st.SessionOnly {
			clearInstallState()
		}
		s.SetTakeoverMode("observe")
		message = "已切换到观察模式（当前不接管系统网络）"
	case "session_pac":
		st := loadInstallState()
		if st != nil && st.SystemProxySet && !st.SessionOnly {
			applySessionManagedProxy(s.cfg, s.certMgr, s.listenPort)
			s.SetTakeoverMode("persistent")
			message = "当前为全局接管配置，已保持持久接管模式生效"
		} else {
			applyTemporarySessionProxy(s.cfg, s.certMgr, s.listenPort)
			s.SetTakeoverMode("session")
			message = "已切换到会话PAC模式（退出自动还原）"
		}
	case "global_install":
		doGlobalInstall(s.certMgr, s.cfg, s.configPath)
		applySessionManagedProxy(s.cfg, s.certMgr, s.listenPort)
		s.SetTakeoverMode("persistent")
		message = "已切换到全局接管模式（持久）"
	case "cleanup":
		st := loadInstallState()
		restoreProxyFromState(st)
		clearAIMonitorPACIfCurrent()
		restoreOrClearEnvVars(st)
		removeAIMonitorIDEProxy()
		if clearConfiguredUpstreamProxy(s.configPath) {
			s.cfg.UpstreamProxy = ""
		}
		clearSavedUpstreamProxy()
		clearInstallState()
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

func (s *ProxyServer) handleConsoleLaunch(w http.ResponseWriter, r *http.Request) {
	var req consoleLaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "请求格式错误"})
		return
	}
	wizardActionMu.Lock()
	defer wizardActionMu.Unlock()

	presetName := strings.ToLower(strings.TrimSpace(req.Preset))
	var command []string
	var preset *launchPreset

	if presetName != "" {
		var err error
		command, preset, err = resolveLaunchCommand(nil, presetName, exec.LookPath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: err.Error()})
			return
		}
		if req.RestartRunning {
			if image, _ := managedPresetProcessImage(preset); image != "" {
				_ = exec.Command("taskkill", "/F", "/IM", image).Run()
				time.Sleep(1200 * time.Millisecond)
			}
		}
	} else {
		bin := strings.TrimSpace(req.CustomBinary)
		if bin == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(consoleActionResponse{Success: false, Message: "请提供 preset 或 custom_binary"})
			return
		}
		if !strings.ContainsAny(bin, `\/:`) {
			if resolved, err := exec.LookPath(bin); err == nil {
				bin = resolved
			}
		}
		command = append([]string{bin}, req.CustomArgs...)
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
		cmd = exec.Command("cmd", "/C", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
