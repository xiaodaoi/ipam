<script setup lang="ts">
import type { LDAPSettings } from '#/api/ipam';

import { computed, onMounted, reactive, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';

import {
  Alert,
  Button,
  Card,
  Input,
  InputPassword,
  message,
  Switch,
  Table,
  Tag,
} from 'ant-design-vue';

import {
  getLDAPSettings,
  listRoles,
  testLDAPLogin,
  updateLDAPSettings,
} from '#/api/ipam';
import PageHeader from '#/components/page-header.vue';
import StatCard from '#/components/stat-card.vue';

defineOptions({ name: 'SystemDirectory' });

const loading = ref(false);
const saving = ref(false);
const testing = ref(false);
const bindPasswordInput = ref('');
const roleNames = ref<string[]>(['admin', 'operator', 'auditor', 'user']);
const testResult = ref<null | {
  displayName: string;
  email?: string;
  groups: string[];
  roles: string[];
  username: string;
}>(null);

const form = reactive<LDAPSettings>({
  enabled: false,
  serverUrl: 'ldap://127.0.0.1:389',
  baseDn: '',
  userFilter: '(uid={username})',
  defaultRole: 'user',
  roleMap: {},
  startTls: false,
  insecureSkipVerify: false,
  groupFilter: '(&(objectClass=groupOfNames)(member={dn}))',
  groupAttr: 'cn',
});

const test = reactive({ username: '', password: '' });
const newGroup = ref('');
const newRole = ref('operator');

const roleMapRows = computed(() =>
  Object.entries(form.roleMap ?? {}).map(([group, role]) => ({ group, role })),
);

function apply(s: LDAPSettings) {
  form.enabled = Boolean(s.enabled);
  form.serverUrl = s.serverUrl || 'ldap://127.0.0.1:389';
  form.baseDn = s.baseDn || '';
  form.userFilter = s.userFilter || '(uid={username})';
  form.defaultRole = s.defaultRole || 'user';
  form.roleMap = { ...s.roleMap };
  form.startTls = Boolean(s.startTls);
  form.insecureSkipVerify = Boolean(s.insecureSkipVerify);
  form.bindDn = s.bindDn || '';
  form.hasBindPassword = Boolean(s.hasBindPassword);
  form.groupBaseDn = s.groupBaseDn || '';
  form.groupFilter =
    s.groupFilter || '(&(objectClass=groupOfNames)(member={dn}))';
  form.groupAttr = s.groupAttr || 'cn';
}

async function load() {
  loading.value = true;
  try {
    apply(await getLDAPSettings());
    try {
      const roles = await listRoles();
      const names = (roles.items ?? []).map((r) => r.name);
      if (names.length > 0) {
        roleNames.value = [...new Set([...roleNames.value, ...names])];
      }
    } catch {
      /* 角色列表失败不阻塞设置读取 */
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '目录设置加载失败');
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (form.enabled && (!form.serverUrl || !form.baseDn)) {
    message.warning('启用目录服务时，服务器地址与 Base DN 必填');
    return;
  }
  saving.value = true;
  try {
    const body: LDAPSettings = { ...form };
    if (bindPasswordInput.value) {
      body.bindPassword = bindPasswordInput.value;
    } else {
      delete body.bindPassword;
    }
    apply(await updateLDAPSettings(body));
    bindPasswordInput.value = '';
    message.success('目录服务设置已保存');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存失败');
  } finally {
    saving.value = false;
  }
}

async function runTest() {
  if (!test.username || !test.password) {
    message.warning('请填写测试账号与口令');
    return;
  }
  testing.value = true;
  testResult.value = null;
  try {
    testResult.value = await testLDAPLogin(test.username, test.password);
    message.success('目录认证成功');
  } catch (error) {
    message.error(error instanceof Error ? error.message : '目录认证失败');
  } finally {
    testing.value = false;
  }
}

function addMapping() {
  const group = newGroup.value.trim();
  if (!group) return;
  form.roleMap = { ...form.roleMap, [group]: newRole.value };
  newGroup.value = '';
}

function removeMapping(group: string) {
  const next = { ...form.roleMap };
  delete next[group];
  form.roleMap = next;
}

onMounted(load);
</script>

<template>
  <div class="ipam-page">
    <PageHeader
      eyebrow="System"
      icon="lucide:network"
      title="目录服务"
      description="对接企业 LDAP / Active Directory：目录账号直接登录，按组自动映射角色；口令始终由目录服务校验，平台不保存目录口令。"
    >
      <template #actions>
        <Button size="small" :loading="loading" @click="load">
          <IconifyIcon icon="lucide:refresh-cw" class="mr-1 size-3.5" />刷新
        </Button>
        <Button type="primary" size="small" :loading="saving" @click="save">
          <IconifyIcon icon="lucide:save" class="mr-1 size-3.5" />保存设置
        </Button>
      </template>
    </PageHeader>

    <div class="ipam-stat-grid">
      <StatCard
        label="目录服务"
        :value="form.enabled ? '已启用' : '未启用'"
        icon="lucide:network"
        :tone="form.enabled ? 'green' : 'violet'"
        :hint="form.enabled ? '目录账号可登录，本地账号不受影响' : '仅本地账号可用'"
      />
      <StatCard
        label="账号来源"
        value="本地 + 目录"
        icon="lucide:users"
        tone="blue"
        hint="目录账号首次登录自动创建影子账号"
      />
      <StatCard
        label="角色映射"
        :value="`${roleMapRows.length} 条`"
        icon="lucide:shield-check"
        tone="cyan"
        :hint="`未匹配组时使用 ${form.defaultRole || 'user'}`"
      />
      <StatCard
        label="口令校验"
        value="实时 bind"
        icon="lucide:key-round"
        tone="amber"
        hint="平台不缓存目录口令"
      />
    </div>

    <Alert
      v-if="form.enabled && form.insecureSkipVerify"
      class="mb-3"
      message="已跳过 TLS 证书校验"
      description="仅建议在测试环境使用；生产环境请配置受信任证书，避免目录口令被中间人截获。"
      show-icon
      type="warning"
    />

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:server" />连接配置
        </span>
      </template>
      <template #extra>
        <Tag class="m-0" :color="form.enabled ? 'green' : 'default'">
          {{ form.enabled ? '已启用' : '已停用' }}
        </Tag>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">启用目录登录</span>
          <div class="flex items-center gap-3">
            <Switch v-model:checked="form.enabled" size="small" />
            <span class="ipam-hint">
              启用后，未知账号会尝试目录认证；本地账号仍走本地口令
            </span>
          </div>
        </div>

        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">服务器地址</span>
          <Input
            v-model:value="form.serverUrl"
            placeholder="ldap://ldap.corp.local:389 或 ldaps://ldap.corp.local:636"
          />
          <span class="ipam-hint">
            建议使用 ldaps:// 或开启 StartTLS；AD 域控通常为 ldap://dc.corp.local:389
          </span>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">StartTLS</span>
          <div class="flex items-center gap-2">
            <Switch v-model:checked="form.startTls" size="small" />
            <span class="ipam-hint">389 端口升级加密</span>
          </div>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">跳过证书校验</span>
          <div class="flex items-center gap-2">
            <Switch v-model:checked="form.insecureSkipVerify" size="small" />
            <span class="ipam-hint">仅测试环境</span>
          </div>
        </div>

        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">Base DN</span>
          <Input v-model:value="form.baseDn" placeholder="dc=corp,dc=local" />
          <span class="ipam-hint">用户搜索根，例如 AD 常用 dc=corp,dc=local</span>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">服务账号 DN（可选）</span>
          <Input
            v-model:value="form.bindDn"
            placeholder="cn=svc-ipam,ou=service,dc=corp,dc=local"
          />
          <span class="ipam-hint">目录不允许匿名搜索时必填</span>
        </div>

        <div class="ipam-form-field">
          <span class="ipam-form-field__label">服务账号口令</span>
          <InputPassword
            v-model:value="bindPasswordInput"
            :placeholder="form.hasBindPassword ? '已配置，留空保持不变' : '未配置'"
            autocomplete="new-password"
          />
          <span class="ipam-hint">
            也可用环境变量 IPAM_LDAP_BIND_PASSWORD 注入，避免落库
          </span>
        </div>

        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">用户过滤器</span>
          <Input
            v-model:value="form.userFilter"
            placeholder="(uid={username})"
          />
          <span class="ipam-hint">
            {username} 为占位符；AD 用 (sAMAccountName={username})，
            OpenLDAP 用 (uid={username})
          </span>
        </div>
      </div>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:shield-check" />组与角色映射
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">未匹配任何组时使用默认角色</span>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">组搜索 Base DN（可选）</span>
          <Input
            v-model:value="form.groupBaseDn"
            placeholder="ou=groups,dc=corp,dc=local"
          />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">组名属性</span>
          <Input v-model:value="form.groupAttr" placeholder="cn" />
        </div>
        <div class="ipam-form-field ipam-form-field--wide">
          <span class="ipam-form-field__label">组过滤器</span>
          <Input
            v-model:value="form.groupFilter"
            placeholder="(&(objectClass=groupOfNames)(member={dn}))"
          />
          <span class="ipam-hint">
            {dn} 为用户 DN；AD 可用 (&(objectClass=group)(member={dn}))
          </span>
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">默认角色</span>
          <Input v-model:value="form.defaultRole" placeholder="user" />
          <span class="ipam-hint">可用角色：{{ roleNames.join(' / ') }}</span>
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <Input
          v-model:value="newGroup"
          placeholder="组标识，如 ipam-ops 或 cn=ipam-ops,ou=groups,dc=corp,dc=local"
          style="max-width: 420px"
          @press-enter="addMapping"
        />
        <Input
          v-model:value="newRole"
          placeholder="角色"
          style="width: 140px"
        />
        <Button size="small" @click="addMapping">
          <IconifyIcon icon="lucide:plus" class="mr-1 size-3.5" />添加映射
        </Button>
      </div>

      <Table
        :data-source="roleMapRows"
        :locale="{ emptyText: '暂无映射，目录账号将使用默认角色' }"
        :pagination="false"
        :row-key="(r: { group: string }) => r.group"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'group'">
            <span class="ipam-mono">{{ record.group }}</span>
          </template>
          <template v-else-if="column.key === 'role'">
            <Tag class="m-0" color="blue">{{ record.role }}</Tag>
          </template>
          <template v-else-if="column.key === 'op'">
            <Button size="small" danger @click="removeMapping(record.group)">
              移除
            </Button>
          </template>
        </template>
        <Table.Column key="group" title="目录组" />
        <Table.Column key="role" title="平台角色" width="160" />
        <Table.Column key="op" title="操作" width="100" />
      </Table>
    </Card>

    <Card>
      <template #title>
        <span class="ipam-card-title">
          <IconifyIcon icon="lucide:plug-zap" />连通性测试
        </span>
      </template>
      <template #extra>
        <span class="ipam-hint">使用指定目录账号执行一次真实 bind，不创建账号</span>
      </template>

      <div class="ipam-form-grid">
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">测试账号</span>
          <Input v-model:value="test.username" placeholder="zhangsan" />
        </div>
        <div class="ipam-form-field">
          <span class="ipam-form-field__label">测试口令</span>
          <InputPassword
            v-model:value="test.password"
            autocomplete="off"
            @press-enter="runTest"
          />
        </div>
      </div>

      <div class="ipam-toolbar ipam-toolbar--plain mt-3">
        <span class="ipam-hint">先保存连接配置，再执行测试</span>
        <span class="ipam-toolbar__spacer"></span>
        <Button type="primary" :loading="testing" @click="runTest">
          <IconifyIcon icon="lucide:plug-zap" class="mr-1 size-3.5" />测试登录
        </Button>
      </div>

      <div v-if="testResult" class="mt-3">
        <div class="ipam-service-value">
          <span>
            {{ testResult.displayName }}（{{ testResult.username }}）
            <span v-if="testResult.email" class="ipam-hint">
              · {{ testResult.email }}
            </span>
          </span>
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <span class="ipam-hint">映射角色：</span>
          <Tag v-for="r in testResult.roles" :key="r" class="m-0" color="green">
            {{ r }}
          </Tag>
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <span class="ipam-hint">所属组：</span>
          <Tag v-for="g in testResult.groups" :key="g" class="m-0">
            {{ g }}
          </Tag>
        </div>
      </div>
    </Card>
  </div>
</template>
