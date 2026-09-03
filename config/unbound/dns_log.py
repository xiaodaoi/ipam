"""
unbound Python 模块 —— DNS 应答结构化日志
从 qstate.return_msg 提取应答区 IP 地址，输出 JSON 供 vector 采集。

用法（unbound.conf）：
  module-config: "python"
  python:
    script: /etc/unbound/dns_log.py

输出格式（info 级别写入 unbound.log）：
  DNS_LOG {"ts":..., "qname":"www.example.com", "qtype":"A", "client_ip":"10.0.0.1",
           "rcode":"NOERROR", "records":[{"type":"A","value":"93.184.216.34","ttl":300}]}
"""

import json
import socket
import struct
import time

from unboundmodule import *

# ---------- RCODE 映射 ----------
RCODE_MAP = {
    0: "NOERROR",
    1: "FORMERR",
    2: "SERVFAIL",
    3: "NXDOMAIN",
    4: "NOTIMPL",
    5: "REFUSED",
    6: "YXDOMAIN",
    7: "YXRRSET",
    8: "NXRRSET",
    9: "NOTAUTH",
    10: "NOTZONE",
}


def _bytes_to_hex(b):
    """将字节转为可读 hex 字符串（备用）。"""
    return b.hex()


def _parse_a_rdata(rdata):
    """从 A 记录 rdata（4 字节网络序）解析为 dotted IP。"""
    if len(rdata) != 4:
        return None
    return socket.inet_ntoa(rdata)


def _parse_aaaa_rdata(rdata):
    """从 AAAA 记录 rdata（16 字节网络序）解析为 IPv6 字符串。"""
    if len(rdata) != 16:
        return None
    return socket.inet_ntop(socket.AF_INET6, rdata)


def _parse_cname_rdata(rdata):
    """从 CNAME/MX/NS 等 rdata 中读取域名（简单 label 解析）。"""
    labels = []
    i = 0
    length = len(rdata)
    while i < length:
        label_len = rdata[i]
        if label_len == 0:
            i += 1
            break
        # 压缩指针检测（最高 2 位为 11）
        if (label_len & 0xC0) == 0xC0:
            break
        i += 1
        if i + label_len > length:
            break
        labels.append(rdata[i:i + label_len].decode("ascii", errors="replace"))
        i += label_len
    return ".".join(labels) if labels else None


def _log_servfail(qstate):
    """return_msg 为空（通常 SERVFAIL）时补日志，避免失败查询缺 resolve 事件。"""
    client_ip = ""
    reply_list = qstate.mesh_info.reply_list
    if reply_list and reply_list.query_reply:
        client_ip = reply_list.query_reply.addr

    log_entry = {
        "ts": time.time(),
        "qname": qstate.qinfo.qname_str.rstrip("."),
        "qtype": qstate.qinfo.qtype_str,
        "client_ip": client_ip,
        "rcode": "SERVFAIL",
        "answer_ip": None,
        "records": [],
    }
    log_info("DNS_LOG " + json.dumps(log_entry, ensure_ascii=False))


def _log_dns_msg(qstate):
    """从 qstate.return_msg 提取解析结果，输出 DNS_LOG JSON。"""
    rep = qstate.return_msg.rep
    qinfo = qstate.return_msg.qinfo

    if not rep:
        return

    # 客户端 IP
    client_ip = ""
    reply_list = qstate.mesh_info.reply_list
    if reply_list and reply_list.query_reply:
        client_ip = reply_list.query_reply.addr

    # RCODE
    rcode_val = rep.flags & 0xF
    rcode_str = RCODE_MAP.get(rcode_val, str(rcode_val))

    # 应答记录
    records = []
    for i in range(rep.rrset_count):
        rr = rep.rrsets[i]
        rk = rr.rk
        d = rr.entry.data

        rr_type = rk.type_str
        hostname = rk.dname_str.rstrip(".")

        for j in range(d.count):
            rdata = d.rr_data[j]
            # 前 2 字节是 RDLENGTH（unsigned short network order）
            if len(rdata) < 2:
                continue
            rdlength = struct.unpack("!H", rdata[:2])[0]
            rdata_body = rdata[2:2 + rdlength]

            value = None
            if rr_type == "A":
                value = _parse_a_rdata(rdata_body)
            elif rr_type == "AAAA":
                value = _parse_aaaa_rdata(rdata_body)
            elif rr_type in ("CNAME", "NS", "MX", "PTR", "SRV"):
                value = _parse_cname_rdata(rdata_body)
            else:
                # 其他类型：hex 表示
                value = _bytes_to_hex(rdata_body)

            if value:
                records.append({
                    "type": rr_type,
                    "hostname": hostname,
                    "value": value,
                    "ttl": d.rr_ttl[j],
                })

    # 构建日志条目
    qname = qinfo.qname_str.rstrip(".")
    qtype = qinfo.qtype_str

    answer_ip = None
    for rec in records:
        if rec["type"] in ("A", "AAAA"):
            answer_ip = rec["value"]
            break

    log_entry = {
        "ts": time.time(),
        "qname": qname,
        "qtype": qtype,
        "client_ip": client_ip,
        "rcode": rcode_str,
        "answer_ip": answer_ip,
        "records": records,
    }

    log_info("DNS_LOG " + json.dumps(log_entry, ensure_ascii=False))


def init(id, cfg):
    """模块初始化（必须返回 True）。"""
    return True


def deinit(id):
    """模块卸载。"""
    return True


def operate(id, event, qstate, qdata):
    """主入口——解析完成后提取应答区并输出结构化日志。"""
    if event in (MODULE_EVENT_NEW, MODULE_EVENT_PASS):
        qstate.ext_state[id] = MODULE_WAIT_MODULE
        return True

    if event == MODULE_EVENT_MODDONE:
        if qstate.return_msg:
            _log_dns_msg(qstate)
        else:
            _log_servfail(qstate)
        qstate.ext_state[id] = MODULE_FINISHED
        return True

    qstate.ext_state[id] = MODULE_ERROR
    return True


def inform_super(id, qstate, superqstate, qdata):
    """子查询完成回调（必需函数，本模块不处理子查询）。"""
    return True
