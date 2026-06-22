# Chain Parse Service 测试用例文档

> 版本: 1.0
> 日期: 2026-03-08
> 状态: Phase 1 测试覆盖

---

## 目录

1. [PancakeSwap V2/V3 (BSC)](#1-pancakeswap-v2v3-bsc)
2. [FourMeme V1/V2 (BSC)](#2-fourmeme-v1v2-bsc)
3. [Uniswap V2/V3 (ETH)](#3-uniswap-v2v3-eth)
4. [PumpFun (Solana)](#4-pumpfun-solana)
5. [PumpSwap (Solana)](#5-pumpswap-solana)
6. [共享工具函数测试](#6-共享工具函数测试)
7. [存储层测试](#7-存储层测试)
8. [API 接口测试](#8-api-接口测试)
9. [集成测试](#9-集成测试)
10. [测试汇总](#10-测试汇总)

---

## 1. PancakeSwap V2/V3 (BSC)

对应文件: `internal/parser/dexs/bsc/pancakeswap.go`
验收标准: AC-PS-1 ~ AC-PS-10

### 1.1 功能测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PS-F-001 | V2 Swap 正常解析 (token0->token1) | BSC 链区块包含 V2 Swap 日志, amount0In > 0, amount1In = 0 | 1. 构造 V2 Swap 日志 data=128字节 2. 调用 ExtractDexData | Transaction.Side="swap", Factory=V2Factory, Amount=amount0In, Price=amount1Out/amount0In, BlockNumber 正确 | P1 |
| PS-F-002 | V2 Swap 正常解析 (token1->token0) | BSC 链区块, amount1In > 0, amount0In = 0 | 1. 构造反向 V2 Swap 日志 2. 调用 ExtractDexData | Amount=amount1In, AmountOut=amount0Out, 方向判断正确 | P1 |
| PS-F-003 | V3 Swap 正常解析 (amount0 正, amount1 负) | BSC 链区块包含 V3 Swap 日志, data=160字节 | 1. 构造 V3 Swap 日志, amount0 为正值(用户付出), amount1 为负值(用户收到) 2. 调用 ExtractDexData | Amount=abs(amount0), Factory=V3Factory, Price 由 sqrtPriceX96 计算, Price > 0 | P1 |
| PS-F-004 | V3 Swap 正常解析 (amount0 负, amount1 正) | BSC 链区块, amount0 负值(二补码), amount1 正值 | 1. 构造 V3 Swap 日志, amount0 二补码编码 2. 调用 ExtractDexData | Amount=abs(amount1) (正值侧作为 amountIn), toSignedInt256 正确处理负数 | P1 |
| PS-F-005 | V2 Mint 事件解析 | BSC 链区块包含 V2 Mint 日志, data=64字节 | 1. 构造 Mint V2 日志 2. 调用 ExtractDexData | Liquidity.Side="add", Amount=amount0+amount1, Factory=V2Factory, Hash 正确 | P1 |
| PS-F-006 | V2 Burn 事件解析 | BSC 链区块包含 V2 Burn 日志, data=64字节 | 1. 构造 Burn V2 日志 2. 调用 ExtractDexData | Liquidity.Side="remove", Amount=amount0+amount1, Hash 正确 | P1 |
| PS-F-007 | V3 Mint 事件解析 | BSC 链区块包含 V3 Mint 日志, data=128字节 | 1. 构造 V3 Mint 日志(sender+amount+amount0+amount1) 2. 调用 ExtractDexData | Liquidity.Side="add", Amount=amount0+amount1(从偏移64和96解析), Factory=V3Factory | P1 |
| PS-F-008 | V3 Burn 事件解析 | BSC 链区块包含 V3 Burn 日志, data=96字节 | 1. 构造 V3 Burn 日志(amount+amount0+amount1) 2. 调用 ExtractDexData | Liquidity.Side="remove", Amount=amount0+amount1(从偏移32和64解析), Factory=V3Factory | P1 |
| PS-F-009 | PairCreated 事件解析 | BSC 链区块包含 PairCreated 日志, topics 含 token0/token1, data=64字节 | 1. 构造 PairCreated 日志 2. 调用 ExtractDexData | Pool.Protocol="pancakeswap", Pool.Addr=data[0:32] 中的地址, Pool.Fee=2500, Tokens 含 token0 和 token1 | P1 |
| PS-F-010 | PoolCreated (V3) 事件解析 | BSC 链区块包含 PoolCreated 日志, topics 含 token0/token1/fee, data=64字节 | 1. 构造 V3 PoolCreated 日志 2. 调用 ExtractDexData | Pool.Addr=data[32:64] 中的地址, Pool.Fee 从 topics[3] 解析, Factory=V3Factory | P1 |
| PS-F-011 | 同一交易多个 Swap 事件 SwapIndex 递增 | 一笔交易中包含 3 个 V2 Swap 日志 (multi-hop) | 1. 构造含 3 个 V2 Swap 的交易 2. 调用 ExtractDexData | SwapIndex 分别为 0, 1, 2; EventIndex 分别为 0, 1, 2 | P1 |
| PS-F-012 | 混合 V2/V3 事件解析 | 一笔交易中同时含 V2 Swap 和 V3 Swap | 1. 构造含 V2+V3 Swap 的交易 2. 调用 ExtractDexData | 结果包含 2 个 Transaction, 第一个 Factory=V2Factory, 第二个 Factory=V3Factory | P2 |
| PS-F-013 | 同一区块多种事件类型 | 一笔交易中含 Swap + Mint 事件 | 1. 构造含 Swap 和 Mint 的交易 2. 调用 ExtractDexData | Transactions.Len=1, Liquidities.Len=1 | P2 |
| PS-F-014 | SupportsBlock 正确链检测 | BSC 区块包含 PancakeSwap 日志 | 调用 SupportsBlock | 返回 true | P1 |
| PS-F-015 | SupportsBlock 错误链返回 false | Solana/Sui 区块 | 调用 SupportsBlock | 返回 false | P1 |
| PS-F-016 | isPancakeSwapLog 全部事件签名识别 | 8 种事件签名 + 1 个未知签名 | 逐个调用 isPancakeSwapLog | 8 种已知签名返回 true, 未知签名返回 false | P2 |
| PS-F-017 | getLogType 正确分类 | 8 种事件签名 | 逐个调用 getLogType | 返回对应字符串: swap_v2/swap_v3/mint/burn/pair_created/pool_created | P2 |
| PS-F-018 | getFactoryAddress V2/V3 区分 | V2 和 V3 事件签名 | 调用 getFactoryAddress | V2 事件返回 V2Factory, V3 事件返回 V3Factory | P2 |
| PS-F-019 | QuoteAssets 配置生效 | 设置 WBNB/USDT/USDC/BUSD QuoteAssets | 1. SetQuoteAssets 2. GetQuoteAssets 3. isQuoteAsset | QuoteAssets 正确存储, isQuoteAsset 对已配置资产返回 true | P2 |

### 1.2 边界测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PS-B-001 | V2 Swap data 恰好 128 字节 | data 长度 = 128 | 构造 128 字节 data 的 V2 Swap 日志 | 正常解析, 不报错 | P1 |
| PS-B-002 | V3 Swap data 恰好 160 字节 | data 长度 = 160 | 构造 160 字节 data 的 V3 Swap 日志 | 正常解析, 不报错 | P1 |
| PS-B-003 | V2 Mint data 恰好 64 字节 | data 长度 = 64 | 构造 64 字节 data 的 Mint 日志 | 正常解析 | P2 |
| PS-B-004 | V3 Mint data 恰好 128 字节 | data 长度 = 128 | 构造 128 字节 data 的 V3 Mint 日志 | 正常解析, amount0 从偏移 64 解析 | P2 |
| PS-B-005 | V3 Burn data 恰好 96 字节 | data 长度 = 96 | 构造 96 字节 data 的 V3 Burn 日志 | 正常解析, amount0 从偏移 32 解析 | P2 |
| PS-B-006 | PairCreated data 恰好 64 字节, topics 恰好 3 个 | 最小必要数据 | 构造最小数据量的 PairCreated 日志 | 正常解析 | P2 |
| PS-B-007 | PoolCreated data 恰好 64 字节, topics 恰好 4 个 | 最小必要数据 | 构造最小数据量的 PoolCreated 日志 | 正常解析 | P2 |
| PS-B-008 | 空 blocks 数组 | blocks = [] | 调用 ExtractDexData 传入空数组 | 返回空 DexData, 无错误 | P2 |
| PS-B-009 | amount0In 和 amount1In 同时非零 | 两个 amount 都 > 0 | 构造此情况的 V2 Swap | 以 amount0In 为主 (direction=0) | P3 |
| PS-B-010 | V3 amount0 和 amount1 都为正值 | 理论上不应出现 | 构造此情况 | amount0 作为 amountIn, 不 panic | P3 |
| PS-B-011 | 极大金额 (接近 uint256 最大值) | amount 接近 2^256-1 | 构造极大金额 | 不溢出, 正常返回 | P3 |
| PS-B-012 | 空交易区块 | 区块无交易 | 调用 ExtractDexData | 返回空 DexData | P2 |

### 1.3 异常测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PS-E-001 | V2 Swap data 不足 128 字节 | data 长度 = 64 | 构造短 data 的 V2 Swap 日志 | 跳过该日志, 不 panic, 记录 Warn 日志 | P1 |
| PS-E-002 | V3 Swap data 不足 160 字节 | data 长度 = 128 | 构造短 data 的 V3 Swap 日志 | 跳过该日志, 不 panic, 记录 Warn 日志 | P1 |
| PS-E-003 | Liquidity data 不足 64 字节 | data 长度 < 64 | 构造短 data 的 Mint/Burn 日志 | 返回 nil, 不 panic | P1 |
| PS-E-004 | PairCreated topics 不足 3 个 | topics 只有 2 个 | 构造少 topics 的 PairCreated | 返回 nil, 不 panic | P1 |
| PS-E-005 | PoolCreated topics 不足 4 个 | topics 只有 3 个 | 构造少 topics 的 PoolCreated | 返回 nil, 不 panic | P1 |
| PS-E-006 | 交易 RawData 为 nil | tx.RawData = nil | 调用 ExtractDexData | 跳过该交易, 返回空结果 | P1 |
| PS-E-007 | 日志无 topics | log.Topics = nil | 构造无 topics 的日志 | isPancakeSwapLog 返回 false, 跳过 | P2 |
| PS-E-008 | 非 PancakeSwap 事件签名 | topic0 为随机值 | 构造随机 topic0 | 不处理, 返回空结果 | P2 |
| PS-E-009 | 不支持的链类型区块 | Solana 区块 | 用 Solana 区块调用 ExtractDexData | 跳过, 返回空结果 | P1 |
| PS-E-010 | V3 Mint data 不足 128 字节 | data 长度 = 96 | 构造短 data 的 V3 Mint | amount0/amount1 为零值, 不 panic | P2 |
| PS-E-011 | V3 Burn data 不足 96 字节 | data 长度 = 64 | 构造短 data 的 V3 Burn | amount0/amount1 为零值, 不 panic | P2 |

---

## 2. FourMeme V1/V2 (BSC)

对应文件: `internal/parser/dexs/bsc/fourmeme.go`
验收标准: AC-FM-1 ~ AC-FM-10

### 2.1 功能测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| FM-F-001 | V2 TokenPurchase 正常解析 | BSC 链, 日志来自 V2 合约地址, data=256字节 | 1. 构造 V2 Purchase 日志 2. 调用 ExtractDexData | Side="buy", Factory=V2Addr, Router=V2Addr, Addr=token地址, From=account地址, Price=weiToFloat(price), Value=weiToFloat(cost) | P1 |
| FM-F-002 | V2 TokenSale 正常解析 | BSC 链, 日志来自 V2 合约地址, data=256字节 | 1. 构造 V2 Sale 日志 2. 调用 ExtractDexData | Side="sell", Extra.Type="sell", Extra.QuoteAddr 包含 fee 信息 | P1 |
| FM-F-003 | V1 TokenPurchase 正常解析 | BSC 链, 日志来自 V1 合约地址, data=128字节 | 1. 构造 V1 Purchase 日志 2. 调用 ExtractDexData | Side="buy", Factory=V1Addr, Price=CalcPrice(etherAmount, tokenAmount), Value=weiToFloat(etherAmount) | P1 |
| FM-F-004 | V1 TokenSale 正常解析 | BSC 链, 日志来自 V1 合约地址, data=128字节 | 1. 构造 V1 Sale 日志 2. 调用 ExtractDexData | Side="sell", Factory=V1Addr | P1 |
| FM-F-005 | V2 TokenCreate 正常解析 | BSC 链, V2 合约, data>=256字节 | 1. 构造 V2 TokenCreate 日志 2. 调用 ExtractDexData | Pool.Protocol="fourmeme", Pool.Factory=V2Addr, Args["version"]=2, Args["creator"]=creator地址, Tokens[0]=token地址 | P1 |
| FM-F-006 | V1 TokenCreate 正常解析 | BSC 链, V1 合约, data>=224字节 | 1. 构造 V1 TokenCreate 日志 2. 调用 ExtractDexData | Pool.Factory=V1Addr, Args["version"]=1, Pool.Fee=0 | P1 |
| FM-F-007 | V2 LiquidityAdded 正常解析 | BSC 链, V2 合约, data=128字节 | 1. 构造 LiquidityAdded 日志 2. 调用 ExtractDexData | Liquidity.Side="add", Addr=base地址, Amount=offers, Value=weiToFloat(funds), Factory=V2Addr | P1 |
| FM-F-008 | V1/V2 合约版本区分 | V1 和 V2 合约地址不同 | 1. 用 V1 地址发起 V1 事件 2. 用 V2 地址发起 V2 事件 | getContractVersion 分别返回 1 和 2 | P1 |
| FM-F-009 | 非 FourMeme 合约日志被忽略 | 日志来自 PancakeSwap 合约 | 1. 构造 PancakeSwap Swap 日志 2. 调用 FourMeme ExtractDexData | isFourMemeLog 返回 false, 结果为空 | P1 |
| FM-F-010 | 同一交易混合 V1+V2 事件 | 一笔交易含 V2 Purchase + V1 Purchase + V2 Sale + V1 Sale | 1. 构造 4 个事件 2. 调用 ExtractDexData | 结果含 4 个 Transaction, Factory 分别正确, SwapIndex 分别为 0/1/2/3 | P1 |
| FM-F-011 | 同一交易全部 V2 事件类型 | 一笔交易含 Purchase + Sale + TokenCreate + LiquidityAdded | 1. 构造全部 V2 事件 2. 调用 ExtractDexData | Transactions=2, Pools=1, Liquidities=1 | P2 |
| FM-F-012 | 多个 Purchase SwapIndex 递增 | 一笔交易含 3 个 V2 TokenPurchase | 1. 构造 3 个 Purchase 2. 调用 ExtractDexData | SwapIndex 分别为 0, 1, 2 | P1 |
| FM-F-013 | SupportsBlock BSC 链正确 | BSC 区块含 FourMeme V2 日志 | 调用 SupportsBlock | 返回 true | P1 |
| FM-F-014 | SupportsBlock 非 BSC 链 | ETH/Solana 区块 | 调用 SupportsBlock | 返回 false | P1 |
| FM-F-015 | V2 Purchase fee 字段提取 | data 含 fee 字段 | 解析 V2 Purchase | Extra.QuoteAddr = "fee:{feeFloat}" 格式 | P2 |

### 2.2 边界测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| FM-B-001 | V2 Purchase data 恰好 256 字节 | data 长度 = 256 | 构造 256 字节 data | 正常解析 | P1 |
| FM-B-002 | V1 Purchase data 恰好 128 字节 | data 长度 = 128 | 构造 128 字节 data | 正常解析 | P1 |
| FM-B-003 | V2 TokenCreate data 恰好 256 字节 | data 长度 = 256 | 构造 256 字节 data | 正常解析 | P2 |
| FM-B-004 | V1 TokenCreate data 恰好 224 字节 | data 长度 = 224 | 构造 224 字节 data | 正常解析 | P2 |
| FM-B-005 | LiquidityAdded data 恰好 128 字节 | data 长度 = 128 | 构造 128 字节 data | 正常解析 | P2 |
| FM-B-006 | wei 金额为 0 | price/amount/cost 全部为 0 | 构造零金额事件 | priceFloat=0, costFloat=0, 不 panic | P3 |
| FM-B-007 | 空区块 | 无交易 | 调用 ExtractDexData | 返回空结果 | P2 |
| FM-B-008 | LiquidityAdded quote 为零地址 | quote = address(0) 表示 BNB | 构造 quote=0x0 | 正常解析, 不影响结果 | P3 |

### 2.3 异常测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| FM-E-001 | V2 Purchase data 不足 256 字节 | data = 128 字节 | 构造短 data | 返回 nil, 记录 Warn 日志, 不 panic | P1 |
| FM-E-002 | V1 Purchase data 不足 128 字节 | data = 64 字节 | 构造短 data | 返回 nil, 记录 Warn 日志, 不 panic | P1 |
| FM-E-003 | V2 TokenCreate data 不足 256 字节 | data = 128 字节 | 构造短 data | 返回 nil, 记录 Warn 日志 | P1 |
| FM-E-004 | V1 TokenCreate data 不足 224 字节 | data = 128 字节 | 构造短 data | 返回 nil, 记录 Warn 日志 | P1 |
| FM-E-005 | LiquidityAdded data 不足 128 字节 | data = 64 字节 | 构造短 data | 返回 nil, 记录 Warn 日志 | P1 |
| FM-E-006 | 日志来自未知合约地址 | log.Address 非 V1/V2 | 构造未知地址日志 | isFourMemeLog 返回 false, 被跳过 | P1 |
| FM-E-007 | 日志无 topics | log.Topics = nil | 构造无 topics 日志 | isFourMemeLog 返回 false | P2 |
| FM-E-008 | RawData 为 nil | tx.RawData = nil | 调用 ExtractDexData | 跳过该交易 | P1 |
| FM-E-009 | 非 BSC 链区块 | ETH 区块 | 调用 ExtractDexData | 跳过, 返回空结果 | P1 |
| FM-E-010 | V2 事件签名 + V1 合约地址 | 签名版本与合约版本不匹配 | 构造版本不匹配日志 | contractVersion 判断排除, 不处理 | P2 |

---

## 3. Uniswap V2/V3 (ETH)

对应文件: `internal/parser/dexs/eth/uniswap.go`
验收标准: AC-UNI-1 ~ AC-UNI-9

### 3.1 功能测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| UNI-F-001 | V2 Swap 正常解析 | ETH 链区块含 V2 Swap 日志 | 1. 构造 V2 Swap 日志 2. 调用 ExtractDexData | Side="swap", Factory=uniswapV2FactoryAddr, Amount>0, EventIndex=logIdx, SwapIndex=0 | P1 |
| UNI-F-002 | V3 Swap 正常解析 (amount0 正) | ETH 链, V3 Swap 日志, amount0 正值 | 1. 构造 V3 Swap 日志 2. 调用 ExtractDexData | 使用 ToSignedInt256 解析, Amount=abs(amount0), Price 由 CalcV3Price(sqrtPriceX96) 计算 | P1 |
| UNI-F-003 | V3 Swap 负数 amount0 处理 (回归) | V3 Swap amount0 为负值(二补码), amount1 为正值 | 1. 构造 amount0 二补码编码 2. 调用 ExtractDexData | Amount=abs(amount1)=6e18 (正值侧作为 amountIn) | P1 |
| UNI-F-004 | V2 Mint 事件 | ETH 链, Mint V2 日志 | 1. 构造 Mint 日志 2. 调用 ExtractDexData | Side="add", Factory=V2Factory, Key 包含 "add_" | P1 |
| UNI-F-005 | V2 Burn 事件 | ETH 链, Burn V2 日志 | 1. 构造 Burn 日志 2. 调用 ExtractDexData | Side="remove", Factory=V2Factory, Key 包含 "remove_" | P1 |
| UNI-F-006 | V3 Mint 事件 | ETH 链, Mint V3 日志 | 1. 构造 V3 Mint 日志 2. 调用 ExtractDexData | Amount=amount0+amount1 (偏移 64+96), Factory=V3Factory | P1 |
| UNI-F-007 | V3 Burn 事件 | ETH 链, Burn V3 日志 | 1. 构造 V3 Burn 日志 2. 调用 ExtractDexData | Amount=amount0+amount1 (偏移 32+64), Factory=V3Factory | P1 |
| UNI-F-008 | PairCreated Pool 地址从 data 解析 (回归) | PairCreated 日志, log.Address=Factory | 1. 构造 PairCreated 2. 调用 ExtractDexData | Pool.Addr = data[0:32] 中地址, 非 log.Address(Factory) | P1 |
| UNI-F-009 | PoolCreated Pool 地址从 data 解析 (回归) | PoolCreated 日志, log.Address=Factory | 1. 构造 PoolCreated 2. 调用 ExtractDexData | Pool.Addr = data[32:64] 中地址, 非 log.Address(Factory), Fee 从 topics[3] 解析 | P1 |
| UNI-F-010 | EventIndex/SwapIndex 递增 (回归) | 一笔交易含 3 个 V2 Swap (multi-hop) | 1. 构造 3 个 Swap 日志 2. 调用 ExtractDexData | SwapIndex=[0,1,2], EventIndex=[0,1,2] | P1 |
| UNI-F-011 | 共享 EVM 辅助函数 | Uniswap 使用 dex.ExtractEVMLogsFromTransaction 等 | 检查代码调用 | 使用 dex 包的共享函数, 无重复代码 | P1 |
| UNI-F-012 | Factory 地址 V2/V3 区分 | V2/V3 不同事件签名 | 调用 getFactoryAddress | V2 事件返回 V2Factory, V3 事件返回 V3Factory | P2 |
| UNI-F-013 | V2 默认手续费 3000 | PairCreated 事件 | 解析 PairCreated | Pool.Fee = 3000 (区别于 PancakeSwap 的 2500) | P2 |
| UNI-F-014 | SupportsBlock ETH 链 | ETH 区块含 Uniswap 日志 | 调用 SupportsBlock | 返回 true | P1 |
| UNI-F-015 | SupportsBlock BSC 链 | BSC 区块含相同事件签名 | 调用 SupportsBlock | 返回 false (Uniswap 限 ETH 链) | P1 |
| UNI-F-016 | 多种事件类型混合 | Swap + Mint 在同一交易 | 1. 构造混合日志 2. 调用 ExtractDexData | Transactions=1, Liquidities=1 | P2 |
| UNI-F-017 | 日志级别为 Debug | 每笔交易处理 | 检查日志调用 | 使用 Debugf 而非 Infof | P3 |

### 3.2 回归测试 (修复验证)

| ID | 场景 | 对应 Bug | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| UNI-R-001 | V3 signed int256 处理 | Bug #1: 缺少 signed int256 | 构造负值 amount0 V3 Swap | ToSignedInt256 正确转换负数, amountIn 为正值侧 | P1 |
| UNI-R-002 | EventIndex 递增 | Bug #2: 硬编码为 0 | 同一交易多个 Swap | EventIndex 从 ExtractEventIndex(log) 获取, 非硬编码 0 | P1 |
| UNI-R-003 | SwapIndex 递增 | Bug #2: 硬编码为 0 | 同一交易多个 Swap | SwapIndex 逐个递增: 0, 1, 2... | P1 |
| UNI-R-004 | 共享日志提取函数 | Bug #3: 重复代码 | 检查调用 dex.ExtractEVMLogsFromTransaction | 无 extractEthereumLogsFromTransaction 重复实现 | P2 |
| UNI-R-005 | Pool 地址从 data 解析 | Bug #5: 使用 log.Address | PairCreated/PoolCreated | Pool.Addr 从 data 而非 log.Address 获取 | P1 |

### 3.3 边界测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| UNI-B-001 | V2 Swap data 恰好 128 字节 | data 长度 = 128 | 正常构造 | 正常解析 | P2 |
| UNI-B-002 | V3 Swap data 恰好 160 字节 | data 长度 = 160 | 正常构造 | 正常解析 | P2 |
| UNI-B-003 | 空区块 | blocks = [] | 调用 ExtractDexData | 返回空结果 | P2 |

### 3.4 异常测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| UNI-E-001 | V2 Swap data 不足 128 字节 | data = 64 字节 | 构造短 data | 跳过, 记录 Warn, 不 panic | P1 |
| UNI-E-002 | V3 Swap data 不足 160 字节 | data = 128 字节 | 构造短 data | 跳过, 记录 Warn, 不 panic | P1 |
| UNI-E-003 | RawData 为 nil | tx.RawData = nil | 调用 ExtractDexData | 跳过该交易 | P1 |
| UNI-E-004 | 不支持的链 (Solana) | Solana 区块 | 调用 ExtractDexData | 跳过, 返回空结果 | P1 |
| UNI-E-005 | 日志无 topics | log.Topics = nil | 构造无 topics | isUniswapLog 返回 false | P2 |
| UNI-E-006 | Liquidity data 不足 64 字节 | data < 64 | 构造短 Mint/Burn | 返回 nil | P2 |

---

## 4. PumpFun (Solana)

对应文件: `internal/parser/dexs/solanadex/pumpfun.go`
验收标准: AC-PF-1 ~ AC-PF-10

### 4.1 功能测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PF-F-001 | TradeEvent 买入解析 | Solana 链, 交易日志含 PumpFun TradeEvent discriminator, is_buy=true | 1. 构造 TradeEvent 数据(89+字节) 2. 编码为 base64 放入日志 3. 调用 ExtractDexData | Side="buy", Addr=mint, From=user, Amount=solAmountBig, Price=solAmount/tokenAmount/1e3, Value=LamportsToSOL(solAmount) | P1 |
| PF-F-002 | TradeEvent 卖出解析 | is_buy=false | 构造 TradeEvent, is_buy=0 | Side="sell" | P1 |
| PF-F-003 | CreateEvent 正常解析 | 交易日志含 CreateEvent discriminator | 1. 构造 CreateEvent 数据(name+symbol+uri+mint+bonding_curve+user) 2. 调用 ExtractDexData | 返回 Pool + Token; Pool.Protocol="pumpfun", Pool.Addr=bondingCurve, Pool.Tokens={0:mint, 1:SOL_NATIVE_MINT}; Token.Decimals=6 | P1 |
| PF-F-004 | CompleteEvent 毕业解析 | 交易日志含 CompleteEvent discriminator | 1. 构造 CompleteEvent 数据(user+mint+bonding_curve+timestamp=104字节) 2. 调用 ExtractDexData | Liquidity.Side="graduate", Addr=bondingCurve, From=user, Key 包含 "graduate_" | P1 |
| PF-F-005 | CreateEvent 同时生成 Pool 和 Token | CreateEvent 数据完整 | 调用 ExtractDexData | dexData.Pools.Len=1, dexData.Tokens.Len=1 | P1 |
| PF-F-006 | TradeEvent timestamp 覆盖 | TradeEvent 中 timestamp > 0 | 解析 TradeEvent | Transaction.Time = event 中的 timestamp, 非 tx.Timestamp | P2 |
| PF-F-007 | SupportsBlock Solana 链 | Solana 区块含 PumpFun 事件 | 调用 SupportsBlock | 返回 true | P1 |
| PF-F-008 | SupportsBlock 非 Solana 链 | BSC/ETH 区块 | 调用 SupportsBlock | 返回 false | P1 |
| PF-F-009 | Program ID 正确 | PumpFun extractor | 检查 Router/Factory 字段 | Router=Factory="6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P" | P2 |
| PF-F-010 | CreateEvent URI 存入 Args | CreateEvent 含 URI | 解析 CreateEvent | Pool.Args["uri"] = uri 字符串 | P3 |
| PF-F-011 | Token 精度为 6 | CreateEvent 解析 | 检查 Token 结果 | Token.Decimals = 6 | P2 |
| PF-F-012 | 多个事件 SwapIndex 递增 | 同一交易含多个 TradeEvent | 构造多个 TradeEvent | SwapIndex 递增: 0, 1, 2 | P1 |
| PF-F-013 | SOL 价格计算 | solAmount=1000000000 (1 SOL), tokenAmount=1000000 (1 token, 6 decimals) | 解析 TradeEvent | Price = 1000000000/1000000/1000 = 0.001, Value = 1.0 | P1 |

### 4.2 字节解析测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PF-P-001 | ParseU64LE 正常解析 | 8 字节小端序数据 | 调用 ParseU64LE | 正确解析为 uint64 | P1 |
| PF-P-002 | ParseI64LE 正常解析 | 8 字节小端序有符号数据 | 调用 ParseI64LE | 正确解析为 int64 (含负值) | P1 |
| PF-P-003 | ParsePubkey 正常解析 | 32 字节公钥 | 调用 ParsePubkey | 返回 base58 编码字符串 | P1 |
| PF-P-004 | ParseString 正常解析 | 4 字节长度前缀 + UTF-8 数据 | 调用 ParseString | 返回正确字符串和新偏移量 | P1 |
| PF-P-005 | ParseBool 正常解析 | 1 字节, 0=false, 非零=true | 调用 ParseBool | 正确返回布尔值 | P1 |
| PF-P-006 | ParseU64LE 越界 | offset+8 > len(data) | 调用 ParseU64LE | 返回 0, offset 不变 | P1 |
| PF-P-007 | ParsePubkey 越界 | offset+32 > len(data) | 调用 ParsePubkey | 返回空字符串, offset 不变 | P1 |
| PF-P-008 | ParseString 长度前缀越界 | offset+4 > len(data) | 调用 ParseString | 返回空字符串, offset 不变 | P1 |
| PF-P-009 | ParseString 内容越界 | 长度前缀指向超出数据范围 | 构造长度前缀大于剩余数据 | 返回空字符串 | P1 |
| PF-P-010 | ParseI64LE 负值 | 0xFFFFFFFFFFFFFFFF = -1 | 调用 ParseI64LE | 返回 -1 | P2 |
| PF-P-011 | ParseU128LE 正常解析 | 16 字节小端序 | 调用 ParseU128LE | 返回正确 Uint128 | P2 |
| PF-P-012 | ParseU8 正常解析 | 1 字节 | 调用 ParseU8 | 返回正确 uint8 | P3 |
| PF-P-013 | ParseU16LE 正常解析 | 2 字节小端序 | 调用 ParseU16LE | 返回正确 uint16 | P3 |

### 4.3 异常测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PF-E-001 | TradeEvent data 不足 89 字节 | data < 89 | 构造短数据 | 返回 nil, 记录 Debug 日志 | P1 |
| PF-E-002 | CreateEvent data 不足 108 字节 | data < 108 | 构造短数据 | 返回 nil, nil | P1 |
| PF-E-003 | CompleteEvent data 不足 104 字节 | data < 104 | 构造短数据 | 返回 nil | P1 |
| PF-E-004 | TradeEvent mint 解析失败 | data 全零导致 mint 为空 | 构造导致 ParsePubkey 返回空的数据 | 返回 nil | P2 |
| PF-E-005 | CreateEvent mint 或 bonding_curve 为空 | 数据不足导致解析失败 | 构造边界数据 | 返回 nil, nil | P2 |
| PF-E-006 | discriminator 不足 8 字节 | eventData < 8 | 构造 7 字节数据 | 跳过该事件 | P1 |
| PF-E-007 | 非 PumpFun discriminator | discriminator 不匹配任何已知值 | 构造随机 discriminator | 不处理, 跳过 | P2 |
| PF-E-008 | RawData 为 nil | tx.RawData = nil | 调用 ExtractDexData | ExtractSolanaEventData 返回 nil, 跳过 | P1 |
| PF-E-009 | tokenAmount 为 0 | 除零风险 | 构造 tokenAmount=0 | Price = 0, 不 panic | P1 |
| PF-E-010 | 非 Solana 链 | BSC 区块 | 调用 ExtractDexData | 跳过, 返回空结果 | P1 |

---

## 5. PumpSwap (Solana)

对应文件: `internal/parser/dexs/solanadex/pumpswap.go`
验收标准: AC-PSW-1 ~ AC-PSW-10

### 5.1 功能测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PSW-F-001 | BuyEvent 正常解析 | Solana 链, PumpSwap BuyEvent discriminator, data>=160字节 | 1. 构造 BuyEvent 数据 2. 调用 ExtractDexData | Side="buy", Addr=baseMint, Pool=pool, Amount=quoteAmountBig, Price=quoteAmountIn/baseAmountOut/1e3, Value=LamportsToSOL(quoteAmountIn) | P1 |
| PSW-F-002 | SellEvent 正常解析 | SellEvent discriminator, data>=160字节 | 1. 构造 SellEvent 2. 调用 ExtractDexData | Side="sell", Addr=baseMint, Amount=quoteAmountBig, Price=quoteAmountOut/baseAmountIn/1e3, Value=LamportsToSOL(quoteAmountOut) | P1 |
| PSW-F-003 | CreatePoolEvent 正常解析 | CreatePoolEvent discriminator, data>=184字节 | 1. 构造 CreatePoolEvent 2. 调用 ExtractDexData | Pool.Protocol="pumpswap", Pool.Addr=pool, Pool.Tokens={0:baseMint, 1:quoteMint}, Pool.Fee=30, Extra.From=creator | P1 |
| PSW-F-004 | DepositEvent 正常解析 | DepositEvent discriminator, data>=88字节 | 1. 构造 DepositEvent 2. 调用 ExtractDexData | Liquidity.Side="add", Addr=pool, Amount=quoteAmountBig, Value=quoteValue*2 | P1 |
| PSW-F-005 | WithdrawEvent 正常解析 | WithdrawEvent discriminator, data>=88字节 | 1. 构造 WithdrawEvent 2. 调用 ExtractDexData | Liquidity.Side="remove", Addr=pool, Amount=quoteAmountBig, Value=quoteValue*2 | P1 |
| PSW-F-006 | SupportsBlock Solana 链 | Solana 区块含 PumpSwap 事件 | 调用 SupportsBlock | 返回 true | P1 |
| PSW-F-007 | SupportsBlock 非 Solana 链 | BSC/ETH 区块 | 调用 SupportsBlock | 返回 false | P1 |
| PSW-F-008 | 5 种 discriminator 全部识别 | Buy/Sell/CreatePool/Deposit/Withdraw | 逐个验证 MatchDiscriminatorBytes | 全部匹配 | P1 |
| PSW-F-009 | Program ID 正确 | PumpSwap extractor | 检查 Router/Factory | Router=Factory="pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA" | P2 |
| PSW-F-010 | BuyEvent quoteMint 存入 Extra | BuyEvent 含 quoteMint 字段 | 解析 BuyEvent | Extra.QuoteAddr = quoteMint (base58) | P2 |
| PSW-F-011 | 多个 Buy/Sell SwapIndex 递增 | 同一交易含多个 Buy/SellEvent | 构造多个交易事件 | SwapIndex 递增: 0, 1, 2 | P1 |

### 5.2 手续费计算测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PSW-FEE-001 | BuyEvent LP fee + protocol fee 解析 | BuyEvent data 含 lp_fee 和 protocol_fee | 解析 BuyEvent | lp_fee 和 protocol_fee 正确从小端序解析 | P1 |
| PSW-FEE-002 | SellEvent 费率字段解析 | SellEvent data 含 lp_fee 和 protocol_fee | 解析 SellEvent | lp_fee 和 protocol_fee 正确解析 | P1 |
| PSW-FEE-003 | CreatePool 默认费率 30 bps | CreatePoolEvent | 解析 CreatePoolEvent | Pool.Fee = 30 | P2 |
| PSW-FEE-004 | Deposit Value 估算 (双倍 SOL) | DepositEvent quoteAmountIn=1 SOL | 解析 DepositEvent | Value = LamportsToSOL(quoteAmountIn) * 2 | P2 |
| PSW-FEE-005 | Withdraw Value 估算 (双倍 SOL) | WithdrawEvent quoteAmountOut=0.5 SOL | 解析 WithdrawEvent | Value = LamportsToSOL(quoteAmountOut) * 2 | P2 |

### 5.3 异常测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| PSW-E-001 | BuyEvent data 不足 160 字节 | data < 160 | 构造短数据 | 返回 nil, 记录 Debug | P1 |
| PSW-E-002 | SellEvent data 不足 160 字节 | data < 160 | 构造短数据 | 返回 nil | P1 |
| PSW-E-003 | CreatePoolEvent data 不足 184 字节 | data < 184 | 构造短数据 | 返回 nil | P1 |
| PSW-E-004 | DepositEvent data 不足 88 字节 | data < 88 | 构造短数据 | 返回 nil | P1 |
| PSW-E-005 | WithdrawEvent data 不足 88 字节 | data < 88 | 构造短数据 | 返回 nil | P1 |
| PSW-E-006 | BuyEvent pool 或 baseMint 为空 | 数据解析失败 | 构造导致 ParsePubkey 返回空的数据 | 返回 nil | P2 |
| PSW-E-007 | CreatePoolEvent pool/baseMint/quoteMint 为空 | 解析失败 | 构造边界数据 | 返回 nil | P2 |
| PSW-E-008 | DepositEvent pool 为空 | ParsePubkey 失败 | 构造边界数据 | 返回 nil | P2 |
| PSW-E-009 | WithdrawEvent pool 为空 | ParsePubkey 失败 | 构造边界数据 | 返回 nil | P2 |
| PSW-E-010 | baseAmountOut 为 0 (BuyEvent 除零) | baseAmountOut = 0 | 构造零值 | Price = 0, 不 panic | P1 |
| PSW-E-011 | baseAmountIn 为 0 (SellEvent 除零) | baseAmountIn = 0 | 构造零值 | Price = 0, 不 panic | P1 |
| PSW-E-012 | discriminator 不足 8 字节 | eventData < 8 | 构造 7 字节 | 跳过该事件 | P1 |
| PSW-E-013 | RawData 为 nil | tx.RawData = nil | 调用 ExtractDexData | 跳过 | P1 |
| PSW-E-014 | 非 Solana 链 | BSC 区块 | 调用 ExtractDexData | 跳过, 返回空结果 | P1 |

---

## 6. 共享工具函数测试

对应文件: `internal/parser/dexs/utils.go`, `internal/parser/dexs/solana_extractor.go`

### 6.1 EVM 工具函数

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| UTL-EVM-001 | CalcPrice 正常计算 | amountIn=1000, amountOut=3000 | 调用 CalcPrice | 返回 3.0 | P1 |
| UTL-EVM-002 | CalcPrice amountIn 为 nil | amountIn=nil | 调用 CalcPrice | 返回 0.0 | P1 |
| UTL-EVM-003 | CalcPrice amountIn 为 0 | amountIn=0 | 调用 CalcPrice | 返回 0.0 | P1 |
| UTL-EVM-004 | CalcPrice 大数值 | amountIn=1000*1e18, amountOut=2000*1e18 | 调用 CalcPrice | 返回 2.0 (大数精度) | P2 |
| UTL-EVM-005 | CalcV3Price sqrtPriceX96=2^96 | sqrtPriceX96 = 2^96 | 调用 CalcV3Price | 返回 1.0 | P1 |
| UTL-EVM-006 | CalcV3Price sqrtPriceX96=2*2^96 | sqrtPriceX96 = 2 * 2^96 | 调用 CalcV3Price | 返回 4.0 | P1 |
| UTL-EVM-007 | CalcV3Price nil | sqrtPriceX96=nil | 调用 CalcV3Price | 返回 0.0 | P1 |
| UTL-EVM-008 | CalcV3Price 零值 | sqrtPriceX96=0 | 调用 CalcV3Price | 返回 0.0 | P1 |
| UTL-EVM-009 | CalcValue 正常 | amount=1000, price=2.5 | 调用 CalcValue | 返回 2500.0 | P1 |
| UTL-EVM-010 | CalcValue nil/零 | amount=nil 或 price=0 | 调用 CalcValue | 返回 0.0 | P1 |
| UTL-EVM-011 | ToSignedInt256 正值 | 0x0100 (256) | 调用 ToSignedInt256 | 返回 big.Int(256) | P1 |
| UTL-EVM-012 | ToSignedInt256 负值 (-1) | 32 字节全 0xFF | 调用 ToSignedInt256 | 返回 big.Int(-1) | P1 |
| UTL-EVM-013 | ToSignedInt256 零值 | 32 字节全 0x00 | 调用 ToSignedInt256 | 返回 big.Int(0) | P1 |
| UTL-EVM-014 | ToSignedInt256 大负数 (-100) | 二补码编码 | 调用 ToSignedInt256 | 返回 big.Int(-100) | P1 |
| UTL-EVM-015 | ToSignedInt256 空字节 | []byte{} | 调用 ToSignedInt256 | 返回 big.Int(0) | P2 |
| UTL-EVM-016 | ToSignedInt64 正值 | 42 | 调用 ToSignedInt64 | 返回 int64(42) | P2 |
| UTL-EVM-017 | ToSignedInt64 负值 (-1) | 32 字节全 0xFF | 调用 ToSignedInt64 | 返回 int64(-1) | P2 |
| UTL-EVM-018 | ConvertDecimals USDC (6位) | amount=1000000, decimals=6 | 调用 ConvertDecimals | 返回 1.0 | P1 |
| UTL-EVM-019 | ConvertDecimals ETH (18位) | amount=1e18, decimals=18 | 调用 ConvertDecimals | 返回 1.0 | P1 |
| UTL-EVM-020 | ConvertDecimals 零精度 | decimals=0 | 调用 ConvertDecimals | 返回原值 42.0 | P2 |
| UTL-EVM-021 | ConvertDecimals 超大精度 | decimals=255 | 调用 ConvertDecimals | 不 panic, 截断至 77 | P2 |
| UTL-EVM-022 | ExtractEVMLogsFromTransaction nil 交易 | tx=nil | 调用 ExtractEVMLogsFromTransaction | 返回 nil | P1 |
| UTL-EVM-023 | ExtractEVMLogsFromTransaction map+logs | RawData 含 "logs" 字段 | 调用 ExtractEVMLogsFromTransaction | 返回解析后的日志 | P1 |
| UTL-EVM-024 | ExtractEVMLogsFromTransaction map+receipt | RawData 含 "receipt" 字段 | 调用 ExtractEVMLogsFromTransaction | 返回 receipt 中的日志 | P1 |
| UTL-EVM-025 | ExtractEVMLogsFromTransaction 直接 Receipt | RawData 为 *ethtypes.Receipt | 调用 ExtractEVMLogsFromTransaction | 返回 Logs | P1 |
| UTL-EVM-026 | ExtractEVMLogsFromTransaction 直接 Log 切片 | RawData 为 []*ethtypes.Log | 调用 ExtractEVMLogsFromTransaction | 直接返回 | P2 |
| UTL-EVM-027 | ValidateAddress 有效地址 | "0x" + 40 hex 字符 | 调用 ValidateAddress | 返回 true | P2 |
| UTL-EVM-028 | ValidateAddress 无效地址 | 长度不对/无前缀/非hex字符 | 调用 ValidateAddress | 返回 false | P2 |
| UTL-EVM-029 | GetBlockNumber nil 交易 | tx=nil 或 BlockNumber=nil | 调用 GetBlockNumber | 返回 0 | P1 |
| UTL-EVM-030 | ExtractEventIndex | Log.Index=42 | 调用 ExtractEventIndex | 返回 int64(42) | P2 |

### 6.2 Solana 工具函数

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| UTL-SOL-001 | ParseU64LE 正常 (小端序) | [0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00] | 调用 ParseU64LE | 返回 1, offset+8 | P1 |
| UTL-SOL-002 | ParseU64LE 最大值 | 8 字节全 0xFF | 调用 ParseU64LE | 返回 uint64 最大值 | P2 |
| UTL-SOL-003 | ParseU64LE 越界 | offset+8 > len(data) | 调用 ParseU64LE | 返回 0, offset 不变 | P1 |
| UTL-SOL-004 | ParseU64LE 负偏移 | offset < 0 | 调用 ParseU64LE | 返回 0, offset 不变 | P2 |
| UTL-SOL-005 | ParseI64LE 正值 | 小端序正整数 | 调用 ParseI64LE | 返回正值 int64 | P1 |
| UTL-SOL-006 | ParseI64LE 负值 (-1) | 8 字节全 0xFF | 调用 ParseI64LE | 返回 -1 | P1 |
| UTL-SOL-007 | ParseI64LE 越界 | offset+8 > len(data) | 调用 ParseI64LE | 返回 0, offset 不变 | P1 |
| UTL-SOL-008 | ParsePubkey 正常 | 32 字节数据 | 调用 ParsePubkey | 返回 base58 编码字符串, offset+32 | P1 |
| UTL-SOL-009 | ParsePubkey 越界 | offset+32 > len(data) | 调用 ParsePubkey | 返回空字符串, offset 不变 | P1 |
| UTL-SOL-010 | ParseString 正常 | 4 字节长度 + "hello" | 调用 ParseString | 返回 "hello", offset+9 | P1 |
| UTL-SOL-011 | ParseString 空字符串 | 长度前缀 = 0 | 调用 ParseString | 返回 "", offset+4 | P2 |
| UTL-SOL-012 | ParseString 长度越界 | 长度前缀 > 剩余数据 | 调用 ParseString | 返回空字符串 | P1 |
| UTL-SOL-013 | ParseString 头部越界 | offset+4 > len(data) | 调用 ParseString | 返回空字符串, offset 不变 | P1 |
| UTL-SOL-014 | ParseBool true | data[offset] = 1 | 调用 ParseBool | 返回 true, offset+1 | P1 |
| UTL-SOL-015 | ParseBool false | data[offset] = 0 | 调用 ParseBool | 返回 false, offset+1 | P1 |
| UTL-SOL-016 | ParseBool 越界 | offset >= len(data) | 调用 ParseBool | 返回 false, offset 不变 | P2 |
| UTL-SOL-017 | ParseU8 正常 | 1 字节 | 调用 ParseU8 | 返回正确 uint8 | P3 |
| UTL-SOL-018 | ParseU16LE 正常 | 2 字节小端序 | 调用 ParseU16LE | 返回正确 uint16 | P3 |
| UTL-SOL-019 | ParseU128LE 正常 | 16 字节小端序 | 调用 ParseU128LE | 返回正确 Uint128 | P2 |
| UTL-SOL-020 | Uint128.ToBigInt | Low=1, High=0 | 调用 ToBigInt | 返回 big.Int(1) | P2 |
| UTL-SOL-021 | LamportsToSOL | lamports=1000000000 | 调用 LamportsToSOL | 返回 1.0 | P1 |
| UTL-SOL-022 | LamportsToSOL 零值 | lamports=0 | 调用 LamportsToSOL | 返回 0.0 | P2 |
| UTL-SOL-023 | MatchDiscriminatorBytes 匹配 | 相同 8 字节 | 调用 MatchDiscriminatorBytes | 返回 true | P1 |
| UTL-SOL-024 | MatchDiscriminatorBytes 不匹配 | 不同 8 字节 | 调用 MatchDiscriminatorBytes | 返回 false | P1 |
| UTL-SOL-025 | MatchDiscriminatorBytes 长度不足 | data < expected 长度 | 调用 MatchDiscriminatorBytes | 返回 false | P1 |
| UTL-SOL-026 | ExtractSolanaEventData 正常 | RawData 含 "Program data: " 前缀日志 | 调用 ExtractSolanaEventData | 返回 base64 解码后的事件数据 | P1 |
| UTL-SOL-027 | ExtractSolanaEventData nil | tx=nil 或 RawData=nil | 调用 ExtractSolanaEventData | 返回 nil | P1 |
| UTL-SOL-028 | ExtractSolanaEventData 无 Program data | 日志中无 "Program data: " | 调用 ExtractSolanaEventData | 返回 nil | P2 |
| UTL-SOL-029 | ExtractSolanaEventData base64 解码失败 | 无效 base64 | 构造无效 base64 数据 | 跳过该条, 不 panic | P2 |
| UTL-SOL-030 | ExtractSolanaEventData 解码后 < 8 字节 | 有效 base64 但解码后太短 | 构造短数据 | 不加入结果 | P2 |
| UTL-SOL-031 | InsufficientDataError 格式 | Needed=8, Got=4, Field="disc" | 调用 Error() | 返回正确错误消息 | P3 |

---

## 7. 存储层测试

对应模块: PostgreSQL 存储引擎 (types.StorageEngine 接口实现)

### 7.1 CRUD 操作

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| STR-C-001 | 保存 Transaction | 数据库可用, Transaction 对象完整 | 调用 SaveTransaction | 数据持久化成功, 可通过 Hash 查询 | P1 |
| STR-C-002 | 保存 Pool | Pool 对象完整 | 调用 SavePool | Pool 数据持久化, 含 Tokens/Fee/Extra | P1 |
| STR-C-003 | 保存 Liquidity | Liquidity 对象完整 | 调用 SaveLiquidity | 数据持久化, Key 唯一 | P1 |
| STR-C-004 | 保存 Token | Token 对象完整 | 调用 SaveToken | 数据持久化, 含 Name/Symbol/Decimals | P1 |
| STR-C-005 | 按 Hash 查询 Transaction | 已保存的 Transaction | 调用 GetByHash | 返回完整 Transaction 对象 | P1 |
| STR-C-006 | 查询不存在的 Hash | Hash 不在数据库中 | 调用 GetByHash("0xnotexist") | 返回 ErrTransactionNotFound | P1 |
| STR-C-007 | 更新已有 Pool | Pool.Addr 已存在 | 重新保存同地址 Pool | 更新成功, 不重复 | P2 |
| STR-C-008 | 删除 Transaction | 已保存的 Transaction | 调用删除方法 | 数据从数据库移除 | P3 |

### 7.2 批量操作

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| STR-B-001 | 批量保存 Transactions | DexData 含多个 Transaction | 调用批量保存 | 全部持久化, 性能可接受 | P1 |
| STR-B-002 | 批量保存 Pools | DexData 含多个 Pool | 调用批量保存 | 全部持久化 | P1 |
| STR-B-003 | 批量保存 Liquidities | DexData 含多个 Liquidity | 调用批量保存 | 全部持久化, Key 不冲突 | P1 |
| STR-B-004 | 批量保存空数据 | DexData 各字段为空数组 | 调用批量保存 | 无错误, 不执行 SQL | P2 |
| STR-B-005 | 大批量保存 (1000+) | 1000 个 Transaction | 调用批量保存 | 性能可接受 (< 30s), 数据完整 | P3 |
| STR-B-006 | 重复 Key Liquidity | 多条 Liquidity Key 相同 | 调用批量保存 | 按策略 (upsert/skip) 处理, 不报错 | P2 |

### 7.3 异常处理

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| STR-E-001 | 数据库连接断开 | 数据库不可用 | 调用保存操作 | 返回错误, 不 panic | P1 |
| STR-E-002 | 必填字段为空 | Transaction.Hash 为空 | 调用保存 | 返回验证错误 | P2 |
| STR-E-003 | StorageStats 数据库不可用 | 数据库连接中断 | 调用 StorageStats | 返回错误信息 | P2 |

---

## 8. API 接口测试

对应文件: `internal/api/` 目录
路由: `/health`, `/api/v1/transactions/:hash`, `/api/v1/storage/stats`, `/api/v1/progress`, `/api/v1/progress/stats`

### 8.1 Health 接口

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| API-H-001 | 健康检查正常 | Storage 可用 | GET /health | 200, {"status":"ok", "storage":{"status":"ok"}} | P1 |
| API-H-002 | 健康检查 Storage 异常 | Storage 不可用 | GET /health | 200, storage.status="error", 含 error 信息 | P1 |
| API-H-003 | 健康检查含 Tracker | Tracker 已启用且正常 | GET /health | 200, 含 progress_tracker.status="ok" | P2 |
| API-H-004 | 健康检查 Tracker 异常 | Tracker 启用但异常 | GET /health | 200, progress_tracker.status="error" | P2 |

### 8.2 Transaction 接口

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| API-T-001 | 按 Hash 查询正常 | 交易已存储 | GET /api/v1/transactions/0x{hash} | 200, 返回 transaction 对象 | P1 |
| API-T-002 | Hash 不存在 | 交易不在数据库 | GET /api/v1/transactions/0x{notexist} | 404, "transaction not found" | P1 |
| API-T-003 | Hash 参数为空 | 无 hash 路径参数 | GET /api/v1/transactions/ | 400, "transaction hash is required" | P1 |
| API-T-004 | Hash 格式无效 (短) | hash 长度 < 20 且非 hex | GET /api/v1/transactions/abc | 400, "invalid transaction hash format" | P2 |
| API-T-005 | Solana 交易 Hash (base58) | base58 格式 hash, 长度 >= 20 | GET /api/v1/transactions/{base58hash} | 通过格式验证 (长度 >= 20 或匹配 hex pattern) | P2 |
| API-T-006 | 服务内部错误 | Storage 查询异常 | GET /api/v1/transactions/0x{hash} | 500, 含错误信息 | P2 |

### 8.3 Stats 接口

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| API-S-001 | 存储统计正常 | Storage 可用 | GET /api/v1/storage/stats | 200, 返回统计数据 | P1 |
| API-S-002 | 存储统计异常 | Storage 不可用 | GET /api/v1/storage/stats | 500, 含错误信息 | P2 |
| API-S-003 | 进度查询正常 | Tracker 可用 | GET /api/v1/progress | 200, 返回 progress 数据 | P1 |
| API-S-004 | 进度查询 Tracker 不可用 | Tracker 未配置 | GET /api/v1/progress | 503, ErrTrackerUnavailable | P2 |
| API-S-005 | 全局统计正常 | Tracker 可用 | GET /api/v1/progress/stats | 200, 返回全局统计 | P1 |
| API-S-006 | 全局统计 Tracker 不可用 | Tracker 未配置 | GET /api/v1/progress/stats | 503, ErrTrackerUnavailable | P2 |

### 8.4 中间件测试

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| API-M-001 | RequestID 中间件 | 任意请求 | 发送请求 | 响应头含 X-Request-ID | P2 |
| API-M-002 | CORS 中间件 | 配置 AllowOrigins | 发送跨域请求 | 响应含正确 CORS 头 | P2 |
| API-M-003 | Recovery 中间件 | handler panic | 触发 panic 的请求 | 500 响应, 不崩溃 | P1 |
| API-M-004 | Logger 中间件 | 任意请求 | 发送请求 | 日志中记录请求信息 | P3 |

---

## 9. 集成测试

### 9.1 端到端流程

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| INT-001 | BSC 区块 -> PancakeSwap 解析 -> 存储 -> API 查询 | PostgreSQL 可用, 完整 BSC 区块数据 | 1. 获取 BSC 区块 2. PancakeSwapExtractor.ExtractDexData 3. 存储到 PostgreSQL 4. 通过 API 查询 Hash | API 返回完整 Transaction, 字段与解析结果一致 | P1 |
| INT-002 | BSC 区块 -> FourMeme 解析 -> 存储 -> API 查询 | 同上, FourMeme 事件 | 同上流程, 使用 FourMemeExtractor | API 返回 FourMeme Transaction, Side="buy"/"sell" | P1 |
| INT-003 | ETH 区块 -> Uniswap 解析 -> 存储 -> API 查询 | 同上, ETH 区块 | 同上流程, 使用 UniswapExtractor | API 返回 Uniswap Transaction | P1 |
| INT-004 | Solana 区块 -> PumpFun 解析 -> 存储 -> API 查询 | 同上, Solana 区块 | 同上流程, 使用 PumpFunExtractor | API 返回 PumpFun Transaction, Pool 和 Token | P1 |
| INT-005 | Solana 区块 -> PumpSwap 解析 -> 存储 -> API 查询 | 同上, Solana 区块 | 同上流程, 使用 PumpSwapExtractor | API 返回 PumpSwap Transaction | P1 |
| INT-006 | 多 DEX 同区块解析 | BSC 区块含 PancakeSwap + FourMeme 事件 | 1. 分别调用两个 Extractor 2. 合并结果 3. 存储 | 两类事件均正确解析和存储 | P2 |
| INT-007 | 空区块处理 | 区块无 DEX 相关交易 | 全流程执行 | 不产生数据, 不报错, 日志正常 | P2 |
| INT-008 | 大批量区块处理 | 100 个区块, 每个含 10+ 交易 | 批量解析和存储 | 性能可接受 (< 30s), 数据完整 | P3 |

### 9.2 跨模块验证

| ID | 场景 | 前置条件 | 步骤 | 预期结果 | 优先级 |
|----|------|---------|------|---------|-------|
| INT-X-001 | Extractor Factory 注册 | 所有 Extractor 已注册 | 调用 Factory 获取 Extractor | PancakeSwap/FourMeme/Uniswap/PumpFun/PumpSwap 全部可获取 | P1 |
| INT-X-002 | 链类型过滤 | BSC 区块 | 各 Extractor 调用 IsChainSupported | PancakeSwap/FourMeme: true; Uniswap/PumpFun/PumpSwap: false | P1 |
| INT-X-003 | SupportsBlock 互斥性 | 单类型 DEX 区块 | 对所有 Extractor 调用 SupportsBlock | 仅对应 Extractor 返回 true | P2 |
| INT-X-004 | DexData 结构完整性 | ExtractDexData 返回结果 | 检查 DexData 所有字段 | Pools/Transactions/Liquidities/Reserves/Tokens 均为非 nil 数组 | P2 |
| INT-X-005 | 配置文件加载 | configs/bsc.yaml, configs/ethereum.yaml | 加载配置并初始化 Extractor | QuoteAssets 正确设置, 支持链正确 | P2 |

---

## 10. 测试汇总

| 模块 | 功能测试 | 边界测试 | 异常测试 | 回归测试 | 合计 |
|------|---------|---------|---------|---------|------|
| PancakeSwap V2/V3 (BSC) | 19 | 12 | 11 | - | 42 |
| FourMeme V1/V2 (BSC) | 15 | 8 | 10 | - | 33 |
| Uniswap V2/V3 (ETH) | 17 | 3 | 6 | 5 | 31 |
| PumpFun (Solana) | 13 | 13 | 10 | - | 36 |
| PumpSwap (Solana) | 11 | 5 | 14 | - | 30 |
| 共享工具函数 (EVM) | 30 | - | - | - | 30 |
| 共享工具函数 (Solana) | 31 | - | - | - | 31 |
| 存储层 | 8 | 6 | 3 | - | 17 |
| API 接口 | 10 | 4 | - | - | 14 |
| 集成测试 | 8 | 5 | - | - | 13 |
| **总计** | **162** | **56** | **54** | **5** | **277** |

### 测试优先级分布

| 优先级 | 数量 | 占比 | 说明 |
|--------|------|------|------|
| P1 | ~160 | ~58% | 核心功能、数据正确性、安全边界 |
| P2 | ~85 | ~31% | 辅助功能、次要边界、配置验证 |
| P3 | ~32 | ~11% | 性能、日志、极端边界 |

### 现有测试覆盖情况

以下文件已有测试:
- `internal/parser/dexs/utils_test.go` - 共享工具函数 (已覆盖大部分 UTL-EVM 用例)
- `internal/parser/dexs/bsc/pancakeswap_test.go` - PancakeSwap (已覆盖大部分 PS-F 用例)
- `internal/parser/dexs/bsc/fourmeme_test.go` - FourMeme (已覆盖大部分 FM-F 用例)
- `internal/parser/dexs/eth/uniswap_test.go` - Uniswap (已覆盖大部分 UNI-F 和回归用例)
- `internal/parser/dexs/base_extractor_test.go` - 基类测试
- `internal/parser/dexs/evm_extractor_test.go` - EVM 提取器测试
- `internal/parser/dexs/cache_test.go` - 缓存测试
- `internal/parser/dexs/extractor_factory_test.go` - 工厂测试

### 待补充测试

1. **PumpFun/PumpSwap 单元测试** - Solana DEX 尚无 `_test.go` 文件, 需新建 `solanadex/pumpfun_test.go` 和 `solanadex/pumpswap_test.go`
2. **Solana 字节解析工具测试** - `solana_extractor.go` 中的 Parse* 函数需独立测试文件 `solana_extractor_test.go`
3. **存储层测试** - 需要 PostgreSQL mock 或 testcontainers-go
4. **API 接口测试** - 需要 httptest + mock service 层
5. **集成测试** - 需要完整测试环境搭建 (docker-compose 或 testcontainers)
