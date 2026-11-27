/* Project inbound webhook channel admin (project settings → Webhooks tab). */
(function () {
  const WH_AUTH = [
    { id: 'hmac', label: 'HMAC-SHA256（默认）' },
    { id: 'token', label: '静态 Token 头' },
    { id: 'hmac_or_token', label: 'HMAC 或 Token' },
  ];

  let whRequest = 0;
  let whState = { channels: [], selected: '', events: [], reveal: null };

  function whCanManage(project) {
    const uid = Number(state.currentUser?.id) || 0;
    if (!project || !uid) return false;
    if (state.currentUser?.platformAdmin || Number(project.ownerId) === uid) return true;
    return (project.members || []).some((m) => Number(m.userId) === uid && m.role === 'maintainer');
  }

  function whPath(projectID, suffix) {
    return '/api/projects/' + encodeURIComponent(projectID) + '/webhooks' + (suffix || '');
  }

  function projectWebhooksPanelHTML(saved) {
    if (!saved) {
      return paSection('Inbound Webhook', '先保存项目，再配置入站 Webhook 渠道。', `<p class="pa-hint">创建项目后可在此添加渠道。</p>`);
    }
    if (!whCanManage(saved)) {
      return paSection('Inbound Webhook', '仅项目 Owner、维护者或平台管理员可管理 Webhook。', `<p class="pa-hint">你当前没有管理权限。</p>`);
    }
    return paSection(
      'Inbound Webhook',
      '为外部系统提供按项目隔离的入站 URL。Secret 仅在创建或轮换时显示一次。',
      `<div class="pa-rows" data-wh-root>
        <div class="pa-row" data-pa-row-inline>
          <div><div class="pa-label">渠道列表</div><div class="pa-desc">每个渠道有独立 URL 与 Secret。</div></div>
          <button type="button" class="button primary small" data-wh-create>＋ 新建渠道</button>
        </div>
        <div data-wh-list class="pa-hint">正在加载…</div>
        <div data-wh-editor hidden></div>
        <div data-wh-reveal hidden class="pa-section" style="margin-top:12px"></div>
        <div data-wh-events hidden></div>
      </div>`
    );
  }

  async function loadProjectWebhooks() {
    const projectID = state.projectAdminDetailId;
    const host = document.querySelector('[data-wh-list]');
    if (!projectID || projectID === 'new' || !host) return;
    const stamp = ++whRequest;
    try {
      const data = await api(whPath(projectID), { silent: true });
      if (stamp !== whRequest) return;
      whState.channels = data.channels || [];
      renderWebhookList();
    } catch (err) {
      if (stamp !== whRequest) return;
      host.textContent = '加载失败：' + (err?.message || err);
    }
  }

  function renderWebhookList() {
    const host = document.querySelector('[data-wh-list]');
    if (!host) return;
    if (!whState.channels.length) {
      host.innerHTML = '<p class="pa-hint">还没有 Webhook 渠道。</p>';
      return;
    }
    host.innerHTML = `<div class="pa-rows">${whState.channels
      .map((c) => {
        const on = c.enabled !== false;
        return `<div class="pa-row" data-pa-row-inline data-wh-id="${escapeHTML(c.id)}">
          <div>
            <div class="pa-label">${escapeHTML(c.name || c.id)} ${on ? '' : '<span class="pa-chip">已停用</span>'}</div>
            <div class="pa-desc mono">${escapeHTML(c.inboundPath || c.deliveryTokenPrefix || '')} · ${escapeHTML(c.authMode || 'hmac')} · secret ${escapeHTML(c.secretPrefix || '')}…</div>
          </div>
          <span>
            <button type="button" class="button ghost small" data-wh-edit="${escapeHTML(c.id)}">编辑</button>
            <button type="button" class="button ghost small" data-wh-events-btn="${escapeHTML(c.id)}">事件</button>
          </span>
        </div>`;
      })
      .join('')}</div>`;
  }

  function webhookEditorHTML(ch) {
    const workflows = (state.data?.workflows || [])
      .filter((w) => !w.projectId || w.projectId === state.projectAdminDetailId || w.shared || w.generic)
      .map((w) => `<option value="${escapeHTML(w.id)}" ${w.id === (ch.defaultWorkflowId || '') ? 'selected' : ''}>${escapeHTML(w.name || w.id)}</option>`)
      .join('');
    const authOpts = WH_AUTH.map((a) => `<option value="${a.id}" ${a.id === (ch.authMode || 'hmac') ? 'selected' : ''}>${a.label}</option>`).join('');
    const members = (() => {
      const p = (state.data.projects || []).find((x) => x.id === state.projectAdminDetailId) || {};
      const ids = new Set([Number(p.ownerId) || 0]);
      (p.members || []).forEach((m) => ids.add(Number(m.userId) || 0));
      return [...ids]
        .filter(Boolean)
        .map((id) => {
          const u = (state.data.users || []).find((x) => Number(x.id) === id);
          const label = u ? u.chineseName || u.loginName || id : id;
          return `<option value="${id}" ${Number(ch.assigneeUserId) === id ? 'selected' : ''}>${escapeHTML(String(label))}</option>`;
        })
        .join('');
    })();
    return `<div class="pa-rows" data-wh-form="${escapeHTML(ch.id || 'new')}">
      ${paRow('名称', '', `<input class="pa-input" name="whName" value="${escapeHTML(ch.name || '')}" maxlength="128" required>`)}
      ${paRow('描述', '', `<textarea class="pa-input" name="whDesc" rows="2">${escapeHTML(ch.description || '')}</textarea>`, { stack: true })}
      ${paRow('鉴权方式', '默认 HMAC-SHA256（请求头 X-LoopForge-Signature）。', `<select class="select" name="whAuth">${authOpts}</select>`)}
      ${paRow('默认 SOP', '创建需求时绑定；开启自动运行时会 dispatch 该 SOP。', `<select class="select" name="whWorkflow"><option value="">不绑定</option>${workflows}</select>`)}
      ${paRow('负责人', '', `<select class="select" name="whAssignee"><option value="0">项目 Owner</option>${members}</select>`)}
      ${paRow('标题模板', '支持 {{title}}、{{.issue.title}}；留空则回退字段。', `<input class="pa-input mono" name="whTitleTpl" value="${escapeHTML(ch.titleTemplate || '')}" placeholder="{{title}}">`)}
      ${paRow('描述模板', '', `<input class="pa-input mono" name="whDescTpl" value="${escapeHTML(ch.descriptionTemplate || '')}" placeholder="{{body}}">`)}
      ${paRow('自动运行 SOP', '创建需求后立即 dispatch 默认 SOP。', `<label class="pa-switch-wrap"><input type="checkbox" name="whAuto" role="switch" ${ch.autoDispatch ? 'checked' : ''}><span class="pa-switch"></span></label>`, { attrs: ' data-pa-row-inline' })}
      ${paRow('启用', '', `<label class="pa-switch-wrap"><input type="checkbox" name="whEnabled" role="switch" ${ch.enabled === false ? '' : 'checked'}><span class="pa-switch"></span></label>`, { attrs: ' data-pa-row-inline' })}
      <div class="pa-row" data-pa-row-inline>
        <div></div>
        <span>
          ${ch.id ? `<button type="button" class="button ghost small" data-wh-rotate>轮换 Secret</button>` : ''}
          ${ch.id ? `<button type="button" class="button ghost small danger" data-wh-delete>删除</button>` : ''}
          <button type="button" class="button ghost small" data-wh-cancel>取消</button>
          <button type="button" class="button primary small" data-wh-save>保存</button>
        </span>
      </div>
    </div>`;
  }

  function showWebhookEditor(ch) {
    const ed = document.querySelector('[data-wh-editor]');
    if (!ed) return;
    ed.hidden = false;
    ed.innerHTML = webhookEditorHTML(ch || { enabled: true, authMode: 'hmac' });
    whState.selected = ch?.id || 'new';
  }

  function showReveal(payload) {
    const box = document.querySelector('[data-wh-reveal]');
    if (!box) return;
    box.hidden = false;
    const url = payload.inboundURL || payload.inboundPath || payload.channel?.inboundURL || payload.channel?.inboundPath || '';
    const secret = payload.secret || '';
    const token = payload.deliveryToken || '';
    box.innerHTML = `<div class="pa-rows">
      <div class="pa-label">请立即复制（只显示一次）</div>
      <div class="pa-desc">关闭后无法再查看完整 Secret / Delivery Token。</div>
      ${url ? paRow('Inbound URL', '', `<input class="pa-input mono" readonly value="${escapeHTML(url)}" data-wh-copy>`) : ''}
      ${token ? paRow('Delivery Token', '', `<input class="pa-input mono" readonly value="${escapeHTML(token)}" data-wh-copy>`) : ''}
      ${secret ? paRow('Secret', '', `<input class="pa-input mono" readonly value="${escapeHTML(secret)}" data-wh-copy>`) : ''}
      <button type="button" class="button ghost small" data-wh-hide-reveal>我已保存</button>
    </div>`;
  }

  async function saveWebhook() {
    const form = document.querySelector('[data-wh-form]');
    if (!form) return;
    const projectID = state.projectAdminDetailId;
    const id = form.getAttribute('data-wh-form');
    const body = {
      name: form.querySelector('[name=whName]')?.value || '',
      description: form.querySelector('[name=whDesc]')?.value || '',
      authMode: form.querySelector('[name=whAuth]')?.value || 'hmac',
      defaultWorkflowId: form.querySelector('[name=whWorkflow]')?.value || '',
      assigneeUserId: Number(form.querySelector('[name=whAssignee]')?.value) || 0,
      titleTemplate: form.querySelector('[name=whTitleTpl]')?.value || '',
      descriptionTemplate: form.querySelector('[name=whDescTpl]')?.value || '',
      autoDispatch: !!form.querySelector('[name=whAuto]')?.checked,
      enabled: !!form.querySelector('[name=whEnabled]')?.checked,
    };
    try {
      if (id === 'new') {
        const created = await api(whPath(projectID), { method: 'POST', body: JSON.stringify(body) });
        showReveal(created);
        toast('Webhook 渠道已创建', 'ok');
      } else {
        await api(whPath(projectID, '/' + encodeURIComponent(id)), { method: 'PUT', body: JSON.stringify(body) });
        toast('Webhook 渠道已更新', 'ok');
        const ed = document.querySelector('[data-wh-editor]');
        if (ed) ed.hidden = true;
      }
      await loadProjectWebhooks();
    } catch (err) {
      toast(err?.message || String(err), 'error');
    }
  }

  async function rotateWebhook() {
    const id = whState.selected;
    if (!id || id === 'new') return;
    if (!confirm('轮换 Secret？新 Secret 立即生效，旧 Secret 有短暂宽限期。')) return;
    try {
      const data = await api(whPath(state.projectAdminDetailId, '/' + encodeURIComponent(id) + '/rotate-secret'), {
        method: 'POST',
        body: '{}',
      });
      showReveal(data);
      toast('Secret 已轮换', 'ok');
      await loadProjectWebhooks();
    } catch (err) {
      toast(err?.message || String(err), 'error');
    }
  }

  async function deleteWebhook() {
    const id = whState.selected;
    if (!id || id === 'new') return;
    if (!confirm('删除该 Webhook 渠道？此操作不可恢复。')) return;
    try {
      await api(whPath(state.projectAdminDetailId, '/' + encodeURIComponent(id)), { method: 'DELETE' });
      toast('已删除', 'ok');
      const ed = document.querySelector('[data-wh-editor]');
      if (ed) ed.hidden = true;
      await loadProjectWebhooks();
    } catch (err) {
      toast(err?.message || String(err), 'error');
    }
  }

  async function loadWebhookEvents(id) {
    const box = document.querySelector('[data-wh-events]');
    if (!box) return;
    box.hidden = false;
    box.innerHTML = '<p class="pa-hint">加载事件…</p>';
    try {
      const data = await api(whPath(state.projectAdminDetailId, '/' + encodeURIComponent(id) + '/events'), { silent: true });
      const events = data.events || [];
      if (!events.length) {
        box.innerHTML = '<p class="pa-hint">暂无事件。</p>';
        return;
      }
      box.innerHTML =
        '<div class="pa-rows">' +
        events
          .map((e) => {
            const when = e.receivedAt ? new Date(e.receivedAt).toLocaleString() : '';
            return `<div class="pa-row"><div class="pa-label">${escapeHTML(e.status || '')} · ${escapeHTML(e.reason || '')}</div>
              <div class="pa-desc">${escapeHTML(when)} · issue ${escapeHTML(e.issueId || '-')} · ${escapeHTML(e.clientIp || '')}</div></div>`;
          })
          .join('') +
        '</div>';
    } catch (err) {
      box.textContent = '加载失败：' + (err?.message || err);
    }
  }

  function bindProjectWebhooks(root) {
    root?.addEventListener('click', (ev) => {
      const t = ev.target.closest('[data-wh-create],[data-wh-edit],[data-wh-events-btn],[data-wh-save],[data-wh-cancel],[data-wh-rotate],[data-wh-delete],[data-wh-hide-reveal],[data-wh-copy]');
      if (!t) return;
      if (t.hasAttribute('data-wh-create')) {
        showWebhookEditor({ enabled: true, authMode: 'hmac' });
        return;
      }
      if (t.hasAttribute('data-wh-edit')) {
        const id = t.getAttribute('data-wh-edit');
        const ch = whState.channels.find((c) => c.id === id);
        showWebhookEditor(ch || { id, enabled: true });
        return;
      }
      if (t.hasAttribute('data-wh-events-btn')) {
        loadWebhookEvents(t.getAttribute('data-wh-events-btn'));
        return;
      }
      if (t.hasAttribute('data-wh-save')) {
        saveWebhook();
        return;
      }
      if (t.hasAttribute('data-wh-cancel')) {
        const ed = document.querySelector('[data-wh-editor]');
        if (ed) ed.hidden = true;
        return;
      }
      if (t.hasAttribute('data-wh-rotate')) {
        rotateWebhook();
        return;
      }
      if (t.hasAttribute('data-wh-delete')) {
        deleteWebhook();
        return;
      }
      if (t.hasAttribute('data-wh-hide-reveal')) {
        const box = document.querySelector('[data-wh-reveal]');
        if (box) {
          box.hidden = true;
          box.innerHTML = '';
        }
        return;
      }
      if (t.hasAttribute('data-wh-copy') || t.matches('[data-wh-copy]')) {
        const input = t.closest('.pa-row')?.querySelector('input') || t;
        if (input?.value) {
          navigator.clipboard?.writeText(input.value).then(() => toast('已复制', 'ok')).catch(() => {});
        }
      }
    });
  }

  window.projectWebhooksPanelHTML = projectWebhooksPanelHTML;
  window.loadProjectWebhooks = loadProjectWebhooks;
  window.bindProjectWebhooks = bindProjectWebhooks;
})();
