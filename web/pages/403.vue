<script setup lang="ts">
// 403：角色不足（如读者访问 /write）。守卫跳这里，不跳登录页。
definePageMeta({ layout: 'default' })
useHead({ title: '无权限访问 · 墨痕' })
</script>

<template>
  <ErrPage :code="403" msg="无权限访问" sub="这个页面需要更高的账号角色，比如作者才能写文章。" />
</template>
