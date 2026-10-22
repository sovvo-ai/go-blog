<script setup lang="ts">
definePageMeta({ layout: 'default', middleware: 'auth' })
interface Msg { id: string; seq: number; sender_id: string; type: string; content: string; created_at: string }
interface Conv { id: string; peer: any; last_message?: any; unread_count: number; is_muted: boolean }

const meId = ref('')
const convs = ref<Conv[]>([])
const current = ref<Conv | null>(null)
const msgs = ref<Msg[]>([])
const draft = ref('')
const peerRead = ref(0)
const err = ref('')
const bubbleBox = ref<HTMLElement | null>(null)

const auth = useAuthStore()
const loadMe = async () => {
  const u = await auth.fetchMe() // 守卫已确认登录；401 由 api 层单例刷新兜住
  if (u) meId.value = u.id
}
const loadConvs = async () => { try { convs.value = (await api<any>('/api/v1/conversations?limit=50')).items || [] } catch (e: any) { err.value = e?.message || '' } }
const open = async (c: Conv) => {
  current.value = c
  err.value = ''
  try {
    const d = await api<any>(`/api/v1/conversations/${c.id}/messages?after_seq=0&limit=50`)
    msgs.value = d.items || []
    peerRead.value = d.peer_last_read_seq || 0
    if (c.unread_count > 0) {
      const maxSeq = msgs.value.length ? msgs.value[msgs.value.length - 1].seq : 0
      if (maxSeq) {
        // 已读回执失败不影响阅读，单独吞掉
        try { await api(`/api/v1/conversations/${c.id}/read`, { method: 'PUT', body: { last_read_seq: maxSeq } }) } catch {}
      }
      c.unread_count = 0
    }
    nextTick(() => bubbleBox.value?.scrollTo({ top: 1e6 }))
  } catch (e: any) {
    err.value = e?.data?.message || e?.message || '会话加载失败'
  }
}
const fmtTime = (s: string) => new Date(s).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
const fmtDate = (s: string) => new Date(s).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })

const sending = ref(false)
const send = async () => {
  const text = draft.value.trim()
  if (!text || !current.value || sending.value) return
  sending.value = true
  err.value = ''
  try {
    const m = await api<any>(`/api/v1/conversations/${current.value.id}/messages`, {
      method: 'POST', body: { type: 'text', content: text, client_msg_id: crypto.randomUUID().slice(0, 32) }
    })
    msgs.value.push({ id: m.id, seq: m.seq, sender_id: meId.value, type: 'text', content: text, created_at: m.created_at })
    draft.value = ''
    nextTick(() => bubbleBox.value?.scrollTo({ top: 1e6 }))
  } catch (e: any) {
    // 失败保留草稿并给出原因，不能静默丢消息
    err.value = e?.data?.message || e?.message || '发送失败，请重试'
  } finally {
    sending.value = false
  }
}

// WebSocket：一次性票据 + 直连后端（WS 不受 CORS 限制）
let ws: WebSocket | null = null
const connectWs = async () => {
  try {
    const t = await api<any>('/api/v1/ws/ticket', { method: 'POST' })
    const cfg = useRuntimeConfig()
    // 生产环境留空 wsUpstream：由当前域名推导（Nginx 代理 /ws），
    // 开发环境指向后端 8080（Nitro dev server 不代理 WebSocket）
    const explicit = String(cfg.public.wsUpstream || '').replace(/\/$/, '')
    const origin = explicit || `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}`
    ws = new WebSocket(`${origin}/ws?ticket=${encodeURIComponent(t.ticket)}`)
    ws.onmessage = (ev) => {
      try {
        const f = JSON.parse(ev.data)
        if (f.type === 'message.new') {
          const m = f.data.message
          if (current.value && String(f.data.conversation_id) === current.value.id) {
            msgs.value.push(m)
            peerRead.value = Math.max(peerRead.value, 0)
            nextTick(() => bubbleBox.value?.scrollTo({ top: 1e6 }))
            // 标记已读（正在看这个会话）
            api(`/api/v1/conversations/${current.value.id}/read`, { method: 'PUT', body: { last_read_seq: m.seq } })
          }
          loadConvs()
        } else if (f.type === 'message.read') {
          peerRead.value = Math.max(peerRead.value, f.data.last_read_seq)
        } else if (f.type === 'system.kick') {
          err.value = '账号已被禁用，连接已断开'
          ws?.close()
        }
      } catch {}
    }
  } catch {}
}
onUnmounted(() => ws?.close())

onMounted(async () => {
  await loadMe()
  await loadConvs()
  await connectWs()
  if (convs.value.length) await open(convs.value[0])
})
</script>

<template>
  <div class="msg-layout" style="display:flex;height:calc(100vh - 130px);min-height:460px;">
    <aside class="conv-list" style="width:270px;flex-shrink:0;border-right:0.5px solid var(--hairline);overflow-y:auto;">
      <div class="conv-search" style="padding:12px 14px;border-bottom:0.5px solid var(--hairline);">
        <input placeholder="搜索会话…" style="width:100%;height:32px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 10px;font-size:12px;color:var(--ink-4);">
      </div>
      <p v-if="!convs.length" style="padding:20px 14px;font-size:12px;color:var(--ink-4);">
        暂无会话。在文章页点作者名即可发起单聊（V1.1 将支持群聊）。
      </p>
      <p v-if="err && !current" style="padding:12px 14px;font-size:12px;color:var(--danger);">{{ err }}</p>
      <div v-for="c in convs" :key="c.id" class="conv" :class="{ on: current?.id === c.id }"
        style="display:flex;gap:10px;padding:12px 14px;border-bottom:0.5px solid var(--hairline);cursor:pointer;"
        @click="open(c)">
        <div class="ava" style="width:36px;height:36px;border-radius:50%;background:#D3D1C7;color:#fff;font-size:12px;display:flex;align-items:center;justify-content:center;flex-shrink:0;">
          {{ (c.peer?.nickname || '?').slice(0, 1) }}
        </div>
        <div style="min-width:0;flex:1;">
          <div style="display:flex;align-items:baseline;gap:6px;">
            <p class="name serif" :style="`margin:0;font-size:14px;color:var(--ink);${current?.id === c.id ? 'font-weight:500;' : ''}`">{{ c.peer?.nickname }}</p>
            <span style="margin-left:auto;font-size:11px;color:var(--ink-4);">{{ c.last_message ? fmtDate(c.last_message.created_at) : '' }}</span>
          </div>
          <div style="display:flex;align-items:center;gap:6px;">
            <p class="preview" style="margin:2px 0 0;font-size:12px;color:var(--ink-3);flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">
              {{ c.last_message?.preview || '（无消息）' }}
            </p>
            <span v-if="c.unread_count" class="unread-badge" style="background:var(--ink);color:#fff;font-size:10px;border-radius:8px;padding:1px 6px;">{{ c.unread_count }}</span>
          </div>
        </div>
      </div>
    </aside>

    <section class="chat" style="flex:1;min-width:0;display:flex;flex-direction:column;">
      <template v-if="current">
        <div class="chat-head" style="height:50px;border-bottom:0.5px solid var(--hairline);display:flex;align-items:center;gap:10px;padding:0 18px;">
          <div class="ava" style="width:28px;height:28px;border-radius:50%;background:var(--ink);color:#fff;font-size:11px;display:flex;align-items:center;justify-content:center;">
            {{ (current.peer?.nickname || '?').slice(0, 1) }}
          </div>
          <p class="peer serif" style="margin:0;font-size:15px;color:var(--ink);">{{ current.peer?.nickname }}</p>
          <span style="font-size:11px;color:var(--ink-4);">{{ current.peer?.online ? '在线' : '离线' }}</span>
          <div style="margin-left:auto;display:flex;gap:14px;font-size:12px;color:var(--ink-3);">
            <span class="link-u">举报</span>
            <span class="link-u">{{ current.is_muted ? '取消免打扰' : '免打扰' }}</span>
          </div>
        </div>
        <div ref="bubbleBox" class="bubbles" style="flex:1;padding:18px 24px;display:flex;flex-direction:column;gap:14px;overflow-y:auto;background:var(--paper-2);">
          <div v-for="m in msgs" :key="m.id" class="bubble" :class="{ mine: m.sender_id === meId }"
            :style="`max-width:70%;${m.sender_id === meId ? 'align-self:flex-end;text-align:right;' : 'align-self:flex-start;'}`">
            <p style="margin:0 0 4px;font-size:11px;color:var(--ink-4);">
              {{ m.sender_id === meId ? '我' : current.peer?.nickname }} · {{ fmtTime(m.created_at) }}
            </p>
            <div :style="`display:inline-block;padding:9px 13px;font-size:13px;line-height:1.7;text-align:left;${m.sender_id === meId ? 'background:var(--ink);color:#fff;border-radius:8px 2px 8px 8px;' : 'background:#fff;border:0.5px solid #D3D1C7;color:var(--ink);border-radius:2px 8px 8px 8px;'}`">
              {{ m.type === 'image' ? '[图片]' : m.content }}
            </div>
            <p v-if="m.sender_id === meId && m.seq <= peerRead" style="margin:3px 0 0;font-size:11px;color:#D1D5DB;">已读</p>
          </div>
          <p v-if="!msgs.length" style="margin:auto;color:var(--ink-4);font-size:12px;">还没有消息，打个招呼吧。</p>
        </div>
        <div class="composer" style="border-top:0.5px solid var(--hairline);padding:12px 18px;">
          <div style="display:flex;gap:8px;align-items:flex-end;">
            <div style="width:32px;height:32px;border:0.5px solid #D3D1C7;border-radius:2px;display:flex;align-items:center;justify-content:center;font-size:12px;color:var(--ink-3);cursor:pointer;" title="图片消息：先经 /uploads/images (purpose=chat) 上传">图</div>
            <input v-model="draft" @keyup.enter="send" placeholder="输入消息，Enter 发送…"
              style="flex:1;height:38px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 12px;font-size:13px;">
            <button class="btn-dark" style="height:38px;padding:0 20px;" :disabled="sending" @click="send">{{ sending ? '发送中…' : '发送' }}</button>
          </div>
          <p v-if="err" style="margin:6px 0 0;font-size:11px;color:var(--danger);">{{ err }}</p>
          <p style="margin:6px 0 0;font-size:11px;color:#D1D5DB;">HTTP 发送 · WebSocket 接收 · 断线自动补齐 · V1.1 预留群聊</p>
        </div>
      </template>
      <div v-else style="margin:auto;color:var(--ink-4);font-size:13px;">选择左侧会话开始聊天</div>
    </section>
  </div>
</template>
