<script setup lang="ts">
const props = defineProps<{ code: number; msg: string; sub: string }>()
</script>

<template>
  <div class="error-page">
    <p class="code">{{ props.code }}</p>
    <p class="msg">{{ props.msg }}</p>
    <p class="sub">{{ props.sub }}</p>
    <button class="btn-dark" style="height:38px;padding:0 26px;cursor:pointer;" @click="navigateTo('/')">回到首页</button>
  </div>
</template>
