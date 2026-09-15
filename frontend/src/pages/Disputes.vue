<template>
  <div class="page">
    <div class="page-title">
      <h2>{{ userStore.isAdmin ? '争议处理队列' : '我的争议' }}</h2>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-table :data="disputes" border stripe v-loading="loading" empty-text="暂无争议">
      <el-table-column label="争议编号" width="100">
        <template #default="{ row }">#{{ row.id }}</template>
      </el-table-column>
      <el-table-column label="合同" width="110">
        <template #default="{ row }">
          <el-link type="primary" @click="openContract(row.contractId)">合同 #{{ row.contractId }}</el-link>
        </template>
      </el-table-column>
      <el-table-column label="发起人" width="140">
        <template #default="{ row }">{{ row.complainant?.name || ('用户#' + row.complainantId) }}</template>
      </el-table-column>
      <el-table-column prop="reason" label="争议事由" min-width="200" show-overflow-tooltip />
      <el-table-column prop="claim" label="诉求" min-width="160" show-overflow-tooltip />
      <el-table-column label="状态" width="130">
        <template #default="{ row }">
          <StatusBadge :status="row.status" kind="dispute" />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="120" fixed="right">
        <template #default="{ row }">
          <el-button size="small" type="primary" @click="openContract(row.contractId)">
            {{ row.status === 'ruled' ? '查看裁决' : userStore.isAdmin ? '处理' : '查看' }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import type { Dispute } from '../types';
import { disputeApi } from '../api/dispute';
import { useUserStore } from '../stores/user';
import StatusBadge from '../components/common/StatusBadge.vue';

const router = useRouter();
const userStore = useUserStore();
const disputes = ref<Dispute[]>([]);
const loading = ref(false);

async function load() {
  loading.value = true;
  try {
    disputes.value = await disputeApi.list();
  } finally {
    loading.value = false;
  }
}

function openContract(contractId: number) {
  router.push(`/contracts/${contractId}`);
}

onMounted(() => void load());
</script>

<style scoped>
.page-title { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
</style>
