# inbrix 产品 API 参考

数据模型与语义,源码位置:`handlers/web/products.go`、`mailstore/products.go`、`mailstore/product_catalog.go`、`mailstore/product_dimensions.go`。

## 认证

`.env` 中 `API_URL`(如 `https://mail.0122.vip/api`)+ `API_KEY`(`inbrix_live_...`)。
请求头 `Authorization: Bearer <key>`。`GET /api/capabilities` 返回当前用户,冒烟用。

## 端点

| 端点 | 方法 | 说明 |
| --- | --- | --- |
| `/api/product-categories` | GET/POST | 分类列表 / 新建 `{name, parentId?}` |
| `/api/product-categories/:id` | PUT/DELETE | 改名/改父级 / 删除(须无子分类;删除会把产品置为未分类) |
| `/api/product-navigation` | GET | 品牌→分类→计数的导航树 |
| `/api/products` | GET/POST | 搜索(`q/categoryId/limit≤20/offset/brand/uncategorized`)/ 新建 |
| `/api/products/:id` | GET/PUT/DELETE | 详情 / 更新(带 `?version=` 于 DELETE) |
| `/api/product-dimensions` | GET | 共享维度字典(含 productCount) |
| `/api/product-images/:id` | GET | 产品图 |

## ProductInput(POST/PUT 请求体)

```json
{
  "name": "25P 导阀型减压阀",        // 必填,≤200 字符
  "brand": "斯派沙克",               // ≤100
  "model": "25P",                   // ≤160
  "categoryId": "pcat_...",         // 空字符串 = 未分类
  "specification": "## 产品概述\n...",  // Markdown,≤100k 字符
  "version": 7,                     // PUT 必填(乐观锁)
  "catalog": {"dimensions": [...], "variants": [...]}  // 省略 = 保留现有目录
}
```

**PUT 是整体覆盖**:省略 categoryId → 未分类;省略 specification → 清空;省略 catalog → 保留。响应 `{"product": {...}}`,新建返回 201。

## Product(响应)

`id / name / brand / model / categoryId / categoryName / specification / imageUrl / version / updatedAt / catalog`。
搜索命中返回 ProductHit(附 section/heading/snippet);`GET /api/products/:id/read` 分页读长规格(段落由 `#` 标题切分,每段 ≤1800 字)。

## 分类语义

- PUT 只带 `{name}` → 保留父级;`{name, parentId: ""}` → 移到根级;建子类 POST 带 `parentId`。
- 同 owner 分类名唯一;不能把分类移到自己后代下;删除要求无子分类。
- 搜索按 `categoryId` 递归包含整棵子树。

## 目录(catalog)

### dimensions(维度定义,owner 级共享字典)

```json
{"key": "max_pressure", "label": "最高工作压力", "type": "number", "unit": "bar g"}
```

- key 匹配 `^[A-Za-z0-9_-]{1,40}$`;label ≤200 且去空格后小写唯一(同产品内);unit ≤40。
- type 只允许 `text` / `number` / `select`。**避免 select**:产品保存时 `ensureProductDimension(mergeOptions=false)` 对已有 key 直接采用字典定义、不合并选项,跨产品共用 select 会出现 "invalid option" 失败。text/number 无此风险。
- 身份去重:`label(小写)+type+unit` 相同的维度全库合并为一条;同 key 已存在时字典定义优先。改维度类型/单位需先无产品引用;删除同样要求 productCount=0(有 tombstone 机制,删过的 key 不能原样复活)。

### variants(变体)

```json
{"sku": "25P-SG-PN16-DN50", "attributes": {"size": "DN50", "...": "..."},
 "prices": [{"amount": "21726", "currency": "CNY"}]}
```

- 每产品 ≤40 维度、≤500 变体、每变体 ≤50 价格。
- **每个变体必须给所有维度赋非空值**(≤500 字符);number 会经 ParseFloat 规范化;select 值必须在选项表内。
- SKU(不区分大小写)与属性组合(值小写化后 JSON)各不得重复。
- attributes 里的 key 必须都在 dimensions 里("unknown specification attribute")。
- 带 `id` 的变体/价格必须已属于该产品(原地更新保留 ID);不带 id 生成新 ID。

### prices(价格)

- 金额:非负十进制字符串,≤6 位小数,≤1e12。币种**强制 CNY**(空/RMB/¥/人民币 也接受,存为 CNY)。
- 可选条件字段 taxRate(0–1)/taxMode(included|excluded,须配 taxRate)/minQty/maxQty/validFrom/validUntil(YYYY-MM-DD)/source(≤500)/leadTime(≤100)——**本项目不用**,纯金额即可(用户约定,避免前端"旧价格含附加条件"提示)。
- 价格全文检索:变体 SKU 和属性值会进 FTS 索引,可直接按材质等值搜索。

## version 与冲突

`version` 从 1 起,每次保存 +1,无历史表(`INSERT ... ON CONFLICT DO UPDATE` 原地覆盖)。PUT 版本不匹配返回 **409**("product changed; reload"),重新 GET 后重试。DELETE 也要 `?version=`。

## 常见错误

| 状态码 | 含义 | 处理 |
| --- | --- | --- |
| 400 ErrProductInvalid | 校验失败,消息具体 | 按消息修数据 |
| 404 | 产品/分类/维度不存在 | 查 id |
| 409 | version 冲突 | 重新 GET 最新版再 PUT |
