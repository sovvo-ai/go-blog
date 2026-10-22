<script setup lang="ts">
import type { NuxtError } from '#app'
const props = defineProps<{ error: NuxtError }>()
const map: Record<number, { msg: string; sub: string }> = {
  404: { msg: '页面不存在', sub: '可能已被删除，或地址写错了。' },
  410: { msg: '内容已删除', sub: '这篇文章已被作者删除，无法恢复。' },
  403: { msg: '没有权限', sub: '登录或角色不满足访问条件。' }
}
const v = map[props.error.statusCode] || { msg: '出错了', sub: props.error.message || '发生了意外错误。' }
</script>

<template>
  <NuxtLayout>
    <ErrPage :code="error.statusCode" :msg="v.msg" :sub="v.sub" />
  </NuxtLayout>
</template>
