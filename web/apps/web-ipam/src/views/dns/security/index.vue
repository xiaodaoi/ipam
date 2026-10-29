<script setup lang="ts">
import type { DiagnoseRequestRow, DiagnoseResult, DnsSettings } from '#/api/ipam';

import { computed, onMounted, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import { Button, Card, Input, InputNumber, message, Select, Switch, Table, Tag } from 'ant-design-vue';

import { diagnoseDns, getDnsSettings, updateDnsSettings } from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

const settings = ref<DnsSettings>();
const testName = ref('');
const testType = ref<DiagnoseRequestRow['type']>('A');
const testClientIp = ref('');
const testing = ref(false);
const diag = ref<DiagnoseResult>();
const saving = ref(false);
const lastTestAt = ref('');

async function load() {
  settings.value = await getDnsSettings();
}
async function save() {
  if (!settings.value) return;
  saving.value = true;
  try {
    settings.value = await updateDnsSettings(settings.value);
    message.success('安全参数已保存并生效');
  } finally {
    saving.value = false;
  }
}
const QTYPE_OPTIONS = ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'PTR', 'SOA'].map(
  (t) => ({ label: t, value: t }),
);
const RCODE_COLOR: Record<string, string> = {
  NOERROR: 'green',
  NXDOMAIN: 'orange',
  REFUSED: 'red',
  SERVFAIL: 'red',
};

async function runTest() {
  if (!testName.value) {
    message.warning('请输入要查询的域名');
    return;
  }
  testing.value = true;
  try {
    diag.value = await diagnoseDns({ name: testName.value, type: testType.value, clientIp: testClientIp.value || undefined });
    lastTestAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false });
  } catch (error) {
    message.error(error instanceof Error ? error.message : '查询失败');
  } finally {
    testing.value = false;
  }
}
onMounted(load);

const stats = computed(() => {
  const s = settings.value;
  return {
    dnssec: s?.dnssecValidate ?? false,
    rcode: diag.value?.rcode ?? '',
    rrlEnabled: s?.rrlEnabled ?? false,
    rrlRate: s?.rrlRate ?? 0,
    rttMs: diag.value?.rttMs ?? 0,
    tcpOnly: s?.tcpOnly ?? false,
    tested: !!diag.value,
  };
});
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="DNS Service"
      icon="lucide:shield-check"
      title="安全与诊断"
      description="配置应答限速、DNSSEC 校验与传输策略，并通过解析测试台验证真实查询结果。"
    >
      <template #actions>
        <span class="ipam-page-header__meta">
          <IconifyIcon icon="lucide:shield" class="size-3.5" />
          RRL {{ stats.rrlEnabled ? '开启' : '关闭' }} · DNSSEC {{ stats.dnssec ? '开启' : '关闭' }}
        </span>
        <Button size="small" @click="load()">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">保存并生效</Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="应答限速（RRL）"
        :value="stats.rrlEnabled ? '已开启' : '已关闭'"
        icon="lucide:gauge"
        :tone="stats.rrlEnabled ? 'green' : 'amber'"
        :hint="stats.rrlEnabled ? '超限来源按配额丢弃应答' : '关闭时可能被放大攻击利用'"
      />
      <StatCard
        label="限速阈值"
        :value="stats.rrlEnabled ? stats.rrlRate : '—'"
        :unit="stats.rrlEnabled ? '次/秒' : ''"
        icon="lucide:speedometer"
        tone="blue"
        :hint="stats.rrlEnabled ? '按来源 IP 计算的每秒配额' : '开启限速后可配置'"
      />
      <StatCard
        label="DNSSEC 校验"
        :value="stats.dnssec ? '已启用' : '未启用'"
        icon="lucide:badge-check"
        :tone="stats.dnssec ? 'green' : 'violet'"
        :hint="stats.dnssec ? '校验签名链，拒绝被篡改应答' : '建议对外解析开启'"
      />
      <StatCard
        label="最近诊断"
        :value="stats.tested ? stats.rcode : '未测试'"
        :unit="stats.tested ? `${stats.rttMs}ms` : ''"
        icon="lucide:stethoscope"
        :tone="stats.rcode === 'NOERROR' ? 'cyan' : stats.tested ? 'amber' : 'blue'"
        :hint="stats.tested ? `最近 ${lastTestAt} 完成查询` : '在下方测试台发起一次查询'"
      />
    </div>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield" />
          安全策略
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">保存后经 resolver 配置重载生效</span>
      </template>

      <div v-if="settings" class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">启用应答限速（RRL）</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.rrlEnabled" size="small" />
            <span class="ipam-hint">{{ settings.rrlEnabled ? '对超限来源丢弃应答' : '当前不做应答限速' }}</span>
          </div>
        </div>
        <label class="ipam-form-field">
          <span class="ipam-form-field__label">ip-ratelimit（次/秒）</span>
          <InputNumber
            v-model:value="settings.rrlRate"
            :min="10"
            :disabled="!settings.rrlEnabled"
            style="width: 100%"
          />
          <span class="ipam-hint">按来源 IP 计算的每秒应答配额</span>
        </label>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">DNSSEC 校验（B-10）</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.dnssecValidate" size="small" />
            <span class="ipam-hint">{{ settings.dnssecValidate ? '校验签名链完整性' : '不校验应答签名' }}</span>
          </div>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">仅 TCP 响应（防 UDP 放大）</span>
          <div class="flex h-8 items-center gap-2">
            <Switch v-model:checked="settings.tcpOnly" size="small" />
            <span class="ipam-hint">{{ settings.tcpOnly ? 'UDP 查询返回截断，强制 TCP 重试' : 'UDP/TCP 均可应答' }}</span>
          </div>
        </div>
        <div class="ipam-form-hint ipam-form-field--wide">
          限速超限的来源 IP 将被丢弃应答（按 resolver 官方 ip-ratelimit 语义）；仅 TCP 响应会对部分老客户端造成解析延迟。
        </div>
        <div class="ipam-form-field--wide flex justify-end">
          <Button type="primary" :loading="saving" @click="save">
            <IconifyIcon icon="lucide:save" class="mr-1" />保存并生效
          </Button>
        </div>
      </div>
      <div v-else class="ipam-empty ipam-empty--compact">
        <div class="ipam-empty__title">安全参数加载中</div>
        <div class="ipam-empty__desc">若长时间无响应，请检查控制面连通性后重试</div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:terminal-square" />
          解析测试台
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">dig 式实时查询 · 支持模拟来源 IP 命中 view</span>
      </template>

      <div class="ipam-toolbar mb-3">
        <div class="ipam-field">
          <span class="ipam-field__label">查询名称</span>
          <Input
            v-model:value="testName"
            placeholder="如 example.com；PTR 直接填 192.168.0.10"
            style="width: 320px"
            @press-enter="runTest"
          />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">类型</span>
          <Select v-model:value="testType" style="width: 100px" :options="QTYPE_OPTIONS" />
        </div>
        <div class="ipam-field">
          <span class="ipam-field__label">模拟来源 IP</span>
          <Input v-model:value="testClientIp" placeholder="可选，用于验证 view 命中" style="width: 220px" />
        </div>
        <Button type="primary" size="small" :loading="testing" @click="runTest">
          <IconifyIcon icon="lucide:play" class="mr-1" />查询
        </Button>
        <div class="ipam-toolbar__spacer"></div>
        <span v-if="lastTestAt" class="ipam-hint">上次查询 {{ lastTestAt }}</span>
      </div>

      <div v-if="diag">
        <div class="ipam-summary mb-3">
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">响应码</span>
            <span class="ipam-summary__value">
              <Tag class="m-0" :color="RCODE_COLOR[diag.rcode] ?? 'red'">{{ diag.rcode }}</Tag>
            </span>
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">往返耗时</span>
            <span class="ipam-summary__value">{{ diag.rttMs }}<small>ms</small></span>
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">应答服务器</span>
            <span class="ipam-summary__value"><span class="ipam-mono text-xs">{{ diag.server }}</span></span>
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">命中 view</span>
            <span class="ipam-summary__value">{{ diag.viewHint || '默认' }}</span>
          </div>
          <div class="ipam-summary__item">
            <span class="ipam-summary__label">答案数</span>
            <span class="ipam-summary__value">{{ diag.answers.length }}<small>条</small></span>
          </div>
        </div>

        <Table
          v-if="diag.answers.length > 0"
          :data-source="diag.answers.map((a, i) => ({ ...a, key: i }))"
          :columns="[
            { title: '记录名', dataIndex: 'name', minWidth: 240 },
            { title: '类型', dataIndex: 'type', width: 90 },
            { title: 'TTL', dataIndex: 'ttl', width: 90 },
            { title: '值', dataIndex: 'value', minWidth: 220 },
          ]"
          :pagination="false"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'type'">
              <Tag class="m-0" color="blue">{{ record.type }}</Tag>
            </template>
            <template v-else-if="column.dataIndex === 'ttl'">
              <span class="ipam-hint">{{ record.ttl ?? '—' }}</span>
            </template>
            <template v-else-if="column.dataIndex === 'value'">
              <span class="ipam-mono text-xs">{{ record.value }}</span>
            </template>
          </template>
        </Table>
        <div v-else class="ipam-empty ipam-empty--compact">
          <div class="ipam-empty__title">没有答案记录</div>
          <div class="ipam-empty__desc">响应码为 {{ diag.rcode }}，可能是域名不存在、被拦截或上游超时</div>
        </div>
      </div>
      <div v-else class="ipam-empty">
        <div class="ipam-empty__title">尚未发起查询</div>
        <div class="ipam-empty__desc">输入域名与类型后点击「查询」，结果将展示响应码、耗时、view 与答案明细</div>
      </div>
    </Card>
  </div>
</template>
