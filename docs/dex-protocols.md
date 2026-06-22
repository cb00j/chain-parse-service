# DEX 协议技术文档

> 本文档详细描述 chain-parse-service 支持的所有 DEX 协议的技术实现细节，
> 包括合约地址、事件结构、数据解析方式、字段映射规则和价格计算方法。
> 面向需要理解或扩展 DEX 解析器的开发者。

---

## 目录

1. [概述](#1-概述)
2. [EVM 链 DEX 协议](#2-evm-链-dex-协议)
   - 2.1 [PancakeSwap (BSC) - V2/V3](#21-pancakeswap-bsc---v2v3)
   - 2.2 [FourMeme (BSC) - V1/V2](#22-fourmeme-bsc---v1v2)
   - 2.3 [Uniswap (ETH) - V2/V3](#23-uniswap-eth---v2v3)
3. [Solana 链 DEX 协议](#3-solana-链-dex-协议)
   - 3.1 [PumpFun](#31-pumpfun)
   - 3.2 [PumpSwap](#32-pumpswap)
4. [Sui 链 DEX 协议](#4-sui-链-dex-协议)
   - 4.1 [Bluefin](#41-bluefin)
5. [跨 DEX 共享组件](#5-跨-dex-共享组件)
   - 5.1 [EVM 共享工具](#51-evm-共享工具)
   - 5.2 [Solana 共享工具](#52-solana-共享工具)
   - 5.3 [Extractor 继承体系](#53-extractor-继承体系)
6. [附录](#6-附录)
   - A. [QuoteAssets 配置](#a-quoteassets-配置)
   - B. [Model 定义参考](#b-model-定义参考)
   - C. [Side 字段取值规范](#c-side-字段取值规范)

---

## 1. 概述

chain-parse-service 当前支持 6 个 DEX 协议，覆盖 4 条链：

| DEX 协议 | 链 | 类型 | Program/Factory | 实现文件 |
|----------|-----|------|-----------------|---------|
| PancakeSwap V2/V3 | BSC | AMM (Uniswap Fork) | V2: `0xcA143Ce3...` / V3: `0x0BFbCF9f...` | `bsc/pancakeswap.go` |
| FourMeme V1/V2 | BSC | Bonding Curve + DEX 毕业 | V1: `0xEC4549ca...` / V2: `0x5c952063...` | `bsc/fourmeme.go` |
| Uniswap V2/V3 | Ethereum | AMM | V2: `0x5C69bEe7...` / V3: `0x1F984431...` | `eth/uniswap.go` |
| PumpFun | Solana | Bonding Curve | `6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P` | `solanadex/pumpfun.go` |
| PumpSwap | Solana | 恒定乘积 AMM | `pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA` | `solanadex/pumpswap.go` |
| Bluefin | Sui | CLMM (集中流动性) | `0x3492c874c1e3b3e2...` | `suidex/bluefin.go` |

### 支持的事件类型总览

| DEX | Swap/Trade | 池子创建 | 流动性添加 | 流动性移除 | 代币创建 | 毕业事件 |
|-----|-----------|---------|-----------|-----------|---------|---------|
| PancakeSwap | V2 Swap, V3 Swap | PairCreated, PoolCreated | Mint V2/V3 | Burn V2/V3 | - | - |
| FourMeme | TokenPurchase, TokenSale | TokenCreate | LiquidityAdded | - | - | LiquidityAdded (毕业) |
| Uniswap | V2 Swap, V3 Swap | PairCreated, PoolCreated | Mint V2/V3 | Burn V2/V3 | - | - |
| PumpFun | TradeEvent | CreateEvent | - | - | CreateEvent (含Token) | CompleteEvent |
| PumpSwap | BuyEvent, SellEvent | CreatePoolEvent | DepositEvent | WithdrawEvent | - | - |
| Bluefin | AssetSwap, FlashSwap | PoolCreated | LiquidityProvided | LiquidityRemoved | - | - |

---

## 2. EVM 链 DEX 协议

EVM 链 (BSC/Ethereum) 的 DEX 协议共享相同的事件日志 (Event Log) 机制：

- 每条事件包含 `topics[]` (最多 4 个, 第一个为事件签名哈希) 和 `data` (ABI 编码的非索引参数)
- 所有数据为 **大端序 (Big Endian)**，每个字段填充为 32 字节
- 地址类型左填充 12 字节零值至 32 字节

### 2.1 PancakeSwap (BSC) - V2/V3

**实现文件**: `internal/parser/dexs/bsc/pancakeswap.go`
**Extractor 类**: `PancakeSwapExtractor` (继承 `EVMDexExtractor`)
**支持链**: BSC (`types.ChainTypeBSC`)
**协议标识**: `["pancakeswap", "pancakeswap-v2", "pancakeswap-v3"]`

#### 2.1.1 合约地址

| 合约 | 地址 | 版本 |
|------|------|------|
| V2 Factory | `0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73` | V2 |
| V3 Factory | `0x0BFbCF9fa4f9C56B0F40a671Ad40E0805A091865` | V3 |

#### 2.1.2 事件签名

| 事件 | topic0 | 版本 |
|------|--------|------|
| Swap (V2) | `0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822` | V2 |
| Swap (V3) | `0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67` | V3 |
| Mint (V2) | `0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f` | V2 |
| Burn (V2) | `0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496` | V2 |
| Mint (V3) | `0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde` | V3 |
| Burn (V3) | `0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c` | V3 |
| PairCreated | `0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9` | V2 |
| PoolCreated | `0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118` | V3 |

#### 2.1.3 事件数据结构与解析

##### Swap V2

```solidity
event Swap(
    address indexed sender,
    uint256 amount0In,
    uint256 amount1In,
    uint256 amount0Out,
    uint256 amount1Out,
    address indexed to
);
```

**data 布局** (128 bytes):

| 偏移量 | 长度 | 字段 | 类型 | 说明 |
|--------|------|------|------|------|
| `data[0:32]` | 32 | amount0In | uint256 | token0 输入量 |
| `data[32:64]` | 32 | amount1In | uint256 | token1 输入量 |
| `data[64:96]` | 32 | amount0Out | uint256 | token0 输出量 |
| `data[96:128]` | 32 | amount1Out | uint256 | token1 输出量 |

**交易方向判断逻辑**：

```go
// amount0In > 0: 用户付出 token0, 获得 token1
//   amountIn = amount0In, amountOut = amount1Out, direction = 0
// amount0In == 0: 用户付出 token1, 获得 token0
//   amountIn = amount1In, amountOut = amount0Out, direction = 1
```

**价格计算**: `price = CalcPrice(amountIn, amountOut) = amountOut / amountIn`

##### Swap V3

```solidity
event Swap(
    address indexed sender,
    address indexed recipient,
    int256 amount0,
    int256 amount1,
    uint160 sqrtPriceX96,
    uint128 liquidity,
    int24 tick
);
```

**data 布局** (160 bytes):

| 偏移量 | 长度 | 字段 | 类型 | 说明 |
|--------|------|------|------|------|
| `data[0:32]` | 32 | amount0 | **int256** | token0 变化量 (正=流入池子, 负=流出池子) |
| `data[32:64]` | 32 | amount1 | **int256** | token1 变化量 (同上) |
| `data[64:96]` | 32 | sqrtPriceX96 | uint160 | 价格的平方根 * 2^96 |
| `data[96:128]` | 32 | liquidity | uint128 | 当前流动性 |
| `data[128:160]` | 32 | tick | int24 | 当前价格 tick |

**int256 有符号整数解析**：

V3 的 amount0/amount1 是有符号 int256，需要使用 `dex.ToSignedInt256()` 进行二进制补码转换：

```go
// 高位 (b[0]) 的最高 bit 为 1 表示负数
func ToSignedInt256(b []byte) *big.Int {
    v := new(big.Int).SetBytes(b)
    if len(b) > 0 && b[0]&0x80 != 0 {
        max := new(big.Int).Lsh(big.NewInt(1), 256) // 2^256
        v.Sub(v, max)
    }
    return v
}
```

**交易方向判断**：
- `amount0 > 0`: 用户付出 token0 (amountIn = |amount0|, amountOut = |amount1|)
- `amount0 < 0`: 用户收到 token0 (amountIn = |amount1|, amountOut = |amount0|)

**V3 价格计算 (sqrtPriceX96)**：

```
price = (sqrtPriceX96 / 2^96)^2
```

```go
func CalcV3Price(sqrtPriceX96 *big.Int) float64 {
    sq := new(big.Float).SetInt(sqrtPriceX96)
    q96 := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil))
    ratio := new(big.Float).Quo(sq, q96)
    price := new(big.Float).Mul(ratio, ratio)
    f, _ := price.Float64()
    return f
}
```

> **注意**：sqrtPriceX96 表示的是 token0 相对于 token1 的价格。
> 若需要 token1 相对于 token0 的价格，取倒数即可。

##### Mint V2

```solidity
event Mint(address indexed sender, uint256 amount0, uint256 amount1);
```

**data 布局** (64 bytes):

| 偏移量 | 字段 | 类型 |
|--------|------|------|
| `data[0:32]` | amount0 | uint256 |
| `data[32:64]` | amount1 | uint256 |

##### Burn V2

```solidity
event Burn(address indexed sender, uint256 amount0, uint256 amount1, address indexed to);
```

**data 布局** (64 bytes): 与 Mint V2 相同。

##### Mint V3

```solidity
event Mint(
    address sender,
    address indexed owner,
    int24 indexed tickLower,
    int24 indexed tickUpper,
    uint128 amount,
    uint256 amount0,
    uint256 amount1
);
```

**data 布局** (128 bytes):

| 偏移量 | 字段 | 类型 |
|--------|------|------|
| `data[0:32]` | sender | address |
| `data[32:64]` | amount | uint128 (流动性单位) |
| `data[64:96]` | amount0 | uint256 |
| `data[96:128]` | amount1 | uint256 |

##### Burn V3

```solidity
event Burn(
    address indexed owner,
    int24 indexed tickLower,
    int24 indexed tickUpper,
    uint128 amount,
    uint256 amount0,
    uint256 amount1
);
```

**data 布局** (96 bytes):

| 偏移量 | 字段 | 类型 |
|--------|------|------|
| `data[0:32]` | amount | uint128 (流动性单位) |
| `data[32:64]` | amount0 | uint256 |
| `data[64:96]` | amount1 | uint256 |

##### PairCreated (V2)

```solidity
event PairCreated(
    address indexed token0,
    address indexed token1,
    address pair,
    uint256
);
```

- `topics[1]` -> token0 地址
- `topics[2]` -> token1 地址
- `data[0:32]` -> pair (池子) 地址
- V2 默认手续费: **2500 bps (0.25%)**

##### PoolCreated (V3)

```solidity
event PoolCreated(
    address indexed token0,
    address indexed token1,
    uint24 indexed fee,
    int24 tickSpacing,
    address pool
);
```

- `topics[1]` -> token0 地址
- `topics[2]` -> token1 地址
- `topics[3]` -> fee (uint24, 基点)
- `data[0:32]` -> tickSpacing
- `data[32:64]` -> pool (池子) 地址

#### 2.1.4 数据字段映射

##### Swap -> model.Transaction

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| `log.Address` | `Addr`, `Pool` | 池子合约地址 |
| `tx.ToAddress` | `Router` | 路由合约地址 |
| V2/V3 Factory 常量 | `Factory` | 工厂合约地址 |
| `tx.TxHash` | `Hash` | 交易哈希 |
| `tx.FromAddress` | `From` | 交易发起人 |
| `"swap"` | `Side` | 固定值 |
| amountIn | `Amount` | 输入金额 |
| CalcPrice / CalcV3Price | `Price` | 计算价格 |
| CalcValue(amountIn, price) | `Value` | 计算价值 |
| `tx.Timestamp` | `Time` | 时间戳 |
| `log.Index` | `EventIndex` | 日志索引 |
| `tx.TxIndex` | `TxIndex` | 交易索引 |
| 递增计数器 | `SwapIndex` | 同一交易内 swap 序号 |
| `tx.BlockNumber` | `BlockNumber` | 区块号 |

##### Mint/Burn -> model.Liquidity

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| `log.Address` | `Addr`, `Pool` | 池子地址 |
| `tx.ToAddress` | `Router` | 路由合约 |
| `getFactoryAddress(log)` | `Factory` | 根据 topic0 区分 V2/V3 |
| `tx.TxHash` | `Hash` | 交易哈希 |
| `tx.FromAddress` | `From` | 发起人 |
| `"add"` / `"remove"` | `Side` | Mint=add, Burn=remove |
| `amount0 + amount1` | `Amount` | 合计数量 |
| `val0 + val1` | `Value` | 合计价值 |
| `"{hash}_{side}_{logIdx}"` | `Key` | 唯一键 |

##### PairCreated/PoolCreated -> model.Pool

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| data 中解析的地址 | `Addr` | 池子地址 |
| V2/V3 Factory 常量 | `Factory` | 工厂地址 |
| `"pancakeswap"` | `Protocol` | 协议名 |
| `{0: token0, 1: token1}` | `Tokens` | 代币对映射 |
| 2500 (V2) / topics[3] (V3) | `Fee` | 手续费率 (bps) |

#### 2.1.5 事件过滤逻辑

`isPancakeSwapLog()` 函数仅检查 `topic0` 是否匹配已知的 8 种事件签名。
由于 PancakeSwap 是 Uniswap 的 Fork，**事件签名与 Uniswap 完全相同**。
区分方式是通过 Extractor 的 `SupportedChains` 配置 (PancakeSwap 仅支持 BSC)。

#### 2.1.6 SwapIndex 递增机制

同一笔交易中可能包含多个 swap 事件 (如路由交易)。每笔交易处理开始时将 `swapIdx` 初始化为 0，每成功解析一个 swap 事件后递增：

```go
swapIdx := int64(0)
for _, log := range ethLogs {
    // ...
    case "swap_v2":
        if modelTx := p.parseV2Swap(log, &tx, eventIndex, swapIdx); modelTx != nil {
            dexData.Transactions = append(dexData.Transactions, *modelTx)
            swapIdx++
        }
    // ...
}
```

---

### 2.2 FourMeme (BSC) - V1/V2

**实现文件**: `internal/parser/dexs/bsc/fourmeme.go`
**Extractor 类**: `FourMemeExtractor` (继承 `EVMDexExtractor`)
**支持链**: BSC
**协议标识**: `["fourmeme"]`

#### 2.2.1 合约地址

| 合约 | 地址 | 版本 | 时间分界 |
|------|------|------|----------|
| TokenManager V1 | `0xEC4549caDcE5DA21Df6E6422d448034B5233bFbC` | V1 | 2024-09-05 之前 |
| TokenManager V2 | `0x5c952063c7fc8610FFDB798152D69F0B9550762b` | V2 | 2024-09-05 之后 |

> **重要**: FourMeme 的事件过滤必须同时检查 `log.Address` 是否为 V1 或 V2 合约地址。
> 不能仅依赖 topic0，因为其他合约可能发出相同签名的事件。

#### 2.2.2 事件签名

**V2 事件**:

| 事件 | topic0 |
|------|--------|
| TokenCreate | `0x396d5e902b675b032348d3d2e9517ee8f0c4a926603fbc075d3d282ff00cad20` |
| TokenPurchase (买入) | `0x7db52723a3b2cdd6164364b3b766e65e540d7be48ffa89582956d8eaebe62942` |
| TokenSale (卖出) | `0x0a5575b3648bae2210cee56bf33254cc1ddfbc7bf637c0af2ac18b14fb1bae19` |
| LiquidityAdded (毕业) | `0xc18aa71171b358b706fe3dd345299685ba21a5316c66ffa9e319268b033c44b0` |

**V1 事件**:

| 事件 | topic0 |
|------|--------|
| TokenCreate | `0xc60523754e4c8d044ae75f841c3a7f27fefeed24c086155510c2ae0edf538fa0` |
| TokenPurchase (买入) | `0x623b3804fa71d67900d064613da8f94b9617215ee90799290593e1745087ad18` |
| TokenSale (卖出) | `0x3aa3f154f6bf5e3490d1a7205aa8d1412e76d26f9d186830de86fb9309224040` |

#### 2.2.3 Bonding Curve 机制概述

FourMeme 是一个 Meme Token 发射平台，其核心生命周期如下：

1. **代币创建 (TokenCreate)**: 创建者通过合约创建新代币，设定初始参数
2. **内盘交易 (TokenPurchase/TokenSale)**: 用户在 bonding curve 上进行买卖。价格由数学公式 (bonding curve) 自动计算，随购买量上升而上涨
3. **毕业 (LiquidityAdded)**: 当代币在 bonding curve 上积累足够资金后，合约自动将流动性添加到 PancakeSwap 等 DEX，代币从内盘转入外盘
4. **外盘交易**: 毕业后代币在 AMM DEX 上正常交易

#### 2.2.4 事件数据结构与解析

##### TokenPurchase V2 (买入)

```solidity
event TokenPurchase(
    address token,
    address account,
    uint256 price,
    uint256 amount,
    uint256 cost,
    uint256 fee,
    uint256 offers,
    uint256 funds
);
```

**data 布局** (256 bytes, 8 x 32 字节, 全部 non-indexed):

| 偏移量 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `data[0:32]` | token | address | 代币合约地址 |
| `data[32:64]` | account | address | 买家地址 |
| `data[64:96]` | price | uint256 | 单价 (Wei 单位) |
| `data[96:128]` | amount | uint256 | 代币数量 |
| `data[128:160]` | cost | uint256 | BNB 花费 (Wei) |
| `data[160:192]` | fee | uint256 | 手续费 (Wei) |
| `data[192:224]` | offers | uint256 | 当前可售代币余量 |
| `data[224:256]` | funds | uint256 | 当前已筹 BNB 资金 |

**价格处理**: V2 直接提供 price 字段，使用 `weiToFloat(price)` 转换 (除以 10^18)。

##### TokenSale V2 (卖出)

```solidity
event TokenSale(
    address token,
    address account,
    uint256 price,
    uint256 amount,
    uint256 cost,
    uint256 fee,
    uint256 offers,
    uint256 funds
);
```

与 TokenPurchase V2 字段完全相同，仅 Side 映射为 `"sell"`。

##### TokenPurchase V1 (买入)

```solidity
event TokenPurchase(
    address token,
    address account,
    uint256 tokenAmount,
    uint256 etherAmount
);
```

**data 布局** (128 bytes, 4 x 32 字节):

| 偏移量 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `data[0:32]` | token | address | 代币地址 |
| `data[32:64]` | account | address | 买家地址 |
| `data[64:96]` | tokenAmount | uint256 | 代币数量 |
| `data[96:128]` | etherAmount | uint256 | BNB 数量 (Wei) |

**价格计算**: V1 不提供 price 字段，需通过 `CalcPrice(etherAmount, tokenAmount)` 计算。

##### TokenSale V1 (卖出)

与 TokenPurchase V1 字段相同。

##### TokenCreate V2

```solidity
event TokenCreate(
    address creator,
    address token,
    uint256 requestId,
    string name,
    string symbol,
    uint256 totalSupply,
    uint256 launchTime,
    uint256 launchFee
);
```

**data 最小长度**: 256 bytes (含 string 动态编码)

| 偏移量 | 字段 | 类型 |
|--------|------|------|
| `data[0:32]` | creator | address |
| `data[32:64]` | token | address |
| 后续 | requestId, name (offset), symbol (offset), totalSupply, launchTime, launchFee | 动态编码 |

##### TokenCreate V1

```solidity
event TokenCreate(
    address creator,
    address token,
    uint256 requestId,
    string name,
    string symbol,
    uint256 totalSupply,
    uint256 launchTime
);
```

**data 最小长度**: 224 bytes (比 V2 少一个 launchFee 字段)

##### LiquidityAdded V2 (毕业事件)

```solidity
event LiquidityAdded(
    address base,
    uint256 offers,
    address quote,
    uint256 funds
);
```

**data 布局** (128 bytes):

| 偏移量 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `data[0:32]` | base | address | 代币地址 |
| `data[32:64]` | offers | uint256 | 代币数量 |
| `data[64:96]` | quote | address | 报价资产 (address(0) = BNB, 否则 BEP-20) |
| `data[96:128]` | funds | uint256 | BNB 数量 |

#### 2.2.5 数据字段映射

##### TokenPurchase/TokenSale -> model.Transaction

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| token (data[0:32]) | `Addr`, `Pool` | 代币地址即为池子标识 |
| V1/V2 合约地址 | `Router`, `Factory` | 同一个合约地址 |
| `tx.TxHash` | `Hash` | 交易哈希 |
| account (data[32:64]) | `From` | 买/卖方地址 |
| `"buy"` / `"sell"` | `Side` | 方向 |
| amount / tokenAmount | `Amount` | 代币数量 |
| weiToFloat(price) | `Price` | V2 直接使用; V1 通过 CalcPrice 计算 |
| weiToFloat(cost/etherAmount) | `Value` | BNB 价值 |
| `fmt.Sprintf("fee:%.18f", feeFloat)` | `Extra.QuoteAddr` | V2 手续费暂存 |
| 18 | `Extra.TokenDecimals` | BEP-20 默认精度 |

##### TokenCreate -> model.Pool

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| token | `Addr` | 代币地址 |
| V1/V2 合约地址 | `Factory` | 工厂地址 |
| `"fourmeme"` | `Protocol` | 协议名 |
| `{0: tokenAddr}` | `Tokens` | 单币 (无配对) |
| `0` | `Fee` | 无固定手续费 |
| creator | `Args["creator"]` | 创建者 |
| 1 或 2 | `Args["version"]` | 合约版本 |

##### LiquidityAdded -> model.Liquidity

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| base | `Addr`, `Pool` | 代币地址 |
| V2 合约地址 | `Router`, `Factory` | 合约地址 |
| `"add"` | `Side` | 添加流动性 |
| offers | `Amount` | 代币数量 |
| weiToFloat(funds) | `Value` | BNB 价值 |
| `"{hash}_liquidity_{logIdx}"` | `Key` | 唯一键 |

#### 2.2.6 版本区分逻辑

```go
func (f *FourMemeExtractor) getContractVersion(log *ethtypes.Log) int {
    addr := strings.ToLower(log.Address.Hex())
    switch addr {
    case fourMemeV1AddrLower: return 1  // 0xec4549ca...
    case fourMemeV2AddrLower: return 2  // 0x5c952063...
    default: return 0
    }
}
```

解析时同时检查 topic0 和合约版本，确保 V1 事件不会被 V2 解析器处理，反之亦然。

#### 2.2.7 weiToFloat 转换

```go
func weiToFloat(wei *big.Int) float64 {
    return dex.ConvertDecimals(wei, 18) // wei / 10^18
}
```

BSC 上的原生代币 BNB 和 BEP-20 标准代币默认使用 18 位精度。

---

### 2.3 Uniswap (ETH) - V2/V3

**实现文件**: `internal/parser/dexs/eth/uniswap.go`
**Extractor 类**: `UniswapExtractor` (继承 `EVMDexExtractor`)
**支持链**: Ethereum (`types.ChainTypeEthereum`)
**协议标识**: `["uniswap", "uniswap-v2", "uniswap-v3"]`

#### 2.3.1 合约地址

| 合约 | 地址 | 版本 |
|------|------|------|
| V2 Router | `0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D` | V2 |
| V2 Factory | `0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f` | V2 |
| V3 Router | `0xE592427A0AEce92De3Edee1F18E0157C05861564` | V3 |
| V3 Router2 | `0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45` | V3 |
| V3 Factory | `0x1F98431c8aD98523631AE4a59f267346ea31F984` | V3 |

#### 2.3.2 与 PancakeSwap 的 Fork 关系

Uniswap 和 PancakeSwap 共享**完全相同的事件签名 (topic0)**，因为 PancakeSwap 是 Uniswap 的 Fork。
两者的核心差异：

| 差异点 | PancakeSwap (BSC) | Uniswap (ETH) |
|--------|-------------------|---------------|
| V2 Factory | `0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73` | `0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f` |
| V3 Factory | `0x0BFbCF9fa4f9C56B0F40a671Ad40E0805A091865` | `0x1F98431c8aD98523631AE4a59f267346ea31F984` |
| Protocol 名称 | `"pancakeswap"` | `"uniswap"` |
| V2 默认手续费 | **2500 bps (0.25%)** | **3000 bps (0.3%)** |
| 支持链 | BSC | Ethereum |

#### 2.3.3 事件结构与解析

事件数据结构与 PancakeSwap **完全相同** (参见 2.1.3 节)。解析代码结构也相同：

- `parseV2Swap()` - 解析 V2 Swap，判断方向，使用 `CalcPrice()`
- `parseV3Swap()` - 解析 V3 Swap，使用 `ToSignedInt256()` 和 `CalcV3Price()`
- `parseLiquidity()` - 解析 Mint/Burn，区分 V2/V3 data 布局
- `parseV2PairCreated()` - 从 `data[0:32]` 提取 pair 地址
- `parseV3PoolCreated()` - 从 `data[32:64]` 提取 pool 地址，从 `topics[3]` 提取 fee

#### 2.3.4 实现差异细节

1. **Factory 地址判断**: `getFactoryAddress()` 根据 topic0 返回对应的 V2/V3 Factory 地址
2. **Swap 价值估算**: Uniswap 直接使用 `CalcValue(amountIn, price)`，而 PancakeSwap 有额外的 `estimateV2SwapValue()` 方法
3. **日志级别**: 使用 `Debug` 级别记录处理详情，避免生产环境日志过多

---

## 3. Solana 链 DEX 协议

Solana DEX 协议与 EVM 有根本性区别：

| 特性 | EVM (BSC/ETH) | Solana |
|------|--------------|--------|
| 事件机制 | Event Log (topics + data) | Program Log + Anchor Events |
| 字节序 | 大端序 (Big Endian) | **小端序 (Little Endian)** |
| 事件标识 | topic0 (32 字节 Keccak256 哈希) | **Discriminator** (8 字节 SHA256 前缀) |
| 地址编码 | 20 字节 Hex (0x...) | 32 字节 Base58 |
| 数据编码 | ABI Encoding | **Borsh Serialization** |
| 金额单位 | Wei (10^18) | Lamports (10^9 for SOL) |

### Anchor 事件解析流程

1. 从交易日志中查找 `"Program data: "` 前缀的行
2. Base64 解码得到原始字节
3. 前 8 字节为 **discriminator**，通过 `sha256("event:<EventName>")` 的前 8 字节计算
4. 后续字节按 Borsh 序列化格式顺序解析

```go
// 提取事件数据的入口
events := dex.ExtractSolanaEventData(&tx)
for _, eventData := range events {
    disc := eventData[:8]    // discriminator
    payload := eventData[8:] // 实际数据
}
```

### 3.1 PumpFun

**实现文件**: `internal/parser/dexs/solanadex/pumpfun.go`
**Extractor 类**: `PumpFunExtractor` (继承 `SolanaDexExtractor`)
**Program ID**: `6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P`
**协议标识**: `["pumpfun"]`
**类型**: Bonding Curve (Meme Token Launchpad)

#### 3.1.1 Discriminator

| 事件 | Discriminator (bytes) | Discriminator (hex) |
|------|----------------------|---------------------|
| CreateEvent | `[27, 114, 169, 77, 222, 235, 99, 118]` | `1b72a94ddeeb6376` |
| TradeEvent | `[189, 219, 127, 211, 78, 230, 97, 238]` | `bddb7fd34ee661ee` |
| CompleteEvent | `[95, 114, 97, 156, 212, 46, 152, 8]` | `5f72619cd42e9808` |

#### 3.1.2 事件数据结构

##### TradeEvent (买入/卖出)

```rust
TradeEvent {
    mint:                    Pubkey,    // 32 bytes - 代币 Mint 地址
    sol_amount:              u64,       // 8 bytes  - SOL 数量 (lamports)
    token_amount:            u64,       // 8 bytes  - 代币数量
    is_buy:                  bool,      // 1 byte   - true=买入, false=卖出
    user:                    Pubkey,    // 32 bytes - 交易者地址
    timestamp:               i64,       // 8 bytes  - 时间戳
    virtual_sol_reserves:    u64,       // 8 bytes  - 虚拟 SOL 储备
    virtual_token_reserves:  u64,       // 8 bytes  - 虚拟代币储备
    real_sol_reserves:       u64,       // 8 bytes  - 实际 SOL 储备
    real_token_reserves:     u64,       // 8 bytes  - 实际代币储备
    // --- 以下为可选字段 (旧版本可能不存在) ---
    fee_recipient:           Pubkey,    // 32 bytes
    fee_basis_points:        u64,       // 8 bytes
    fee:                     u64,       // 8 bytes
    creator:                 Pubkey,    // 32 bytes
    creator_fee_basis_points: u64,     // 8 bytes
    creator_fee:             u64,       // 8 bytes
}
```

**最小数据长度**: 89 bytes (mint + sol_amount + token_amount + is_buy + user + timestamp)

**Borsh 解析流程** (offset-based):

```go
off := 0
mint, off = dex.ParsePubkey(data, off)       // 32 bytes, base58 编码
solAmount, off = dex.ParseU64LE(data, off)    // 8 bytes, little-endian
tokenAmount, off = dex.ParseU64LE(data, off)  // 8 bytes
isBuy, off = dex.ParseBool(data, off)         // 1 byte
user, off = dex.ParsePubkey(data, off)        // 32 bytes
timestamp, off = dex.ParseI64LE(data, off)    // 8 bytes, signed
```

**价格计算**:

```
price = (solAmount / 1e9) / (tokenAmount / 1e6) = solAmount / tokenAmount / 1e3
```

SOL 精度 9 位，PumpFun 代币精度 6 位，因此除以 10^3 进行精度对齐。

##### CreateEvent (代币创建)

```rust
CreateEvent {
    name:                  String,      // 4-byte LE 长度前缀 + UTF-8
    symbol:                String,      // 同上
    uri:                   String,      // 元数据 URI
    mint:                  Pubkey,      // 32 bytes
    bonding_curve:         Pubkey,      // 32 bytes
    user:                  Pubkey,      // 32 bytes - 创建者
    // --- 可选字段 ---
    creator:               Pubkey,
    timestamp:             i64,
    virtual_token_reserves: u64,
    virtual_sol_reserves:  u64,
    real_token_reserves:   u64,
    token_total_supply:    u64,
}
```

**最小数据长度**: 108 bytes (3 个空 string 各 4 bytes + 3 个 Pubkey 各 32 bytes)

**String 解析 (Borsh 格式)**:

```go
// 4 字节小端序长度 + UTF-8 数据
func ParseString(data []byte, offset int) (string, int) {
    length := binary.LittleEndian.Uint32(data[offset : offset+4])
    offset += 4
    return string(data[offset : offset+length]), offset + length
}
```

**CreateEvent 同时生成两条记录**:
1. `model.Pool` - 池子信息 (bonding_curve 地址作为 Pool.Addr，含 SOL native mint 作为 token1)
2. `model.Token` - 代币元数据 (name, symbol, decimals=6)

##### CompleteEvent (毕业/Bonding Curve 完成)

```rust
CompleteEvent {
    user:          Pubkey,    // 32 bytes - 触发者
    mint:          Pubkey,    // 32 bytes - 代币 Mint
    bonding_curve: Pubkey,    // 32 bytes - Bonding Curve 地址
    timestamp:     i64,       // 8 bytes  - 完成时间戳
}
```

**最小数据长度**: 104 bytes

映射为 `model.Liquidity`，`Side = "graduate"`，表示代币从 bonding curve 毕业。

#### 3.1.3 数据字段映射

##### TradeEvent -> model.Transaction

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| mint | `Addr`, `Pool` | 代币 Mint 地址 (作为 pool 标识) |
| Program ID | `Router`, `Factory` | PumpFun Program ID |
| tx signature | `Hash` | 交易签名 |
| user | `From` | 交易者 |
| is_buy ? "buy" : "sell" | `Side` | 方向 |
| sol_amount (as big.Int) | `Amount` | SOL 数量 (lamports) |
| solAmount / tokenAmount / 1e3 | `Price` | SOL/代币 价格 |
| LamportsToSOL(solAmount) | `Value` | SOL 价值 |
| 6 | `Extra.TokenDecimals` | PumpFun 代币精度 |

##### CreateEvent -> model.Pool + model.Token

**Pool**:

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| bonding_curve | `Addr` | Bonding Curve 地址 |
| Program ID | `Factory` | PumpFun Program ID |
| `"pumpfun"` | `Protocol` | 协议名 |
| `{0: mint, 1: "So11111111111111111111111111111111"}` | `Tokens` | 代币 + SOL |
| 100 | `Fee` | 手续费 1% (100 bps) |
| uri | `Args["uri"]` | 元数据 URI |

**Token**:

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| mint | `Addr` | 代币地址 |
| name | `Name` | 代币名称 |
| symbol | `Symbol` | 代币符号 |
| 6 | `Decimals` | PumpFun 代币默认精度 |

##### CompleteEvent -> model.Liquidity

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| bonding_curve | `Addr`, `Pool` | Bonding Curve 地址 |
| Program ID | `Router`, `Factory` | PumpFun Program ID |
| user | `From` | 触发者 |
| `"graduate"` | `Side` | 毕业 (特殊类型) |
| `"{hash}_graduate_{eventIdx}"` | `Key` | 唯一键 |

---

### 3.2 PumpSwap

**实现文件**: `internal/parser/dexs/solanadex/pumpswap.go`
**Extractor 类**: `PumpSwapExtractor` (继承 `SolanaDexExtractor`)
**Program ID**: `pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA`
**协议标识**: `["pumpswap"]`
**类型**: 恒定乘积 AMM (Constant Product, 类似 Uniswap V2)

#### 3.2.1 Discriminator

| 事件 | Discriminator (bytes) | Discriminator (hex) |
|------|----------------------|---------------------|
| BuyEvent | `[103, 244, 82, 31, 44, 245, 119, 119]` | `67f4521f2cf57777` |
| SellEvent | `[62, 47, 55, 10, 165, 3, 220, 42]` | `3e2f370aa503dc2a` |
| CreatePoolEvent | `[177, 49, 12, 210, 160, 118, 167, 116]` | `b1310cd2a076a774` |
| DepositEvent | `[120, 248, 61, 83, 31, 142, 107, 144]` | `78f83d531f8e6b90` |
| WithdrawEvent | `[22, 9, 133, 26, 160, 44, 71, 192]` | `1609851aa02c47c0` |

#### 3.2.2 费率结构

| 费率类型 | 基点 (bps) | 百分比 |
|---------|-----------|--------|
| LP 手续费 | 20 | 0.20% |
| 协议手续费 | 5 | 0.05% |
| **合计** | **25** | **0.25%** |

> **注意**: 当前实现中 CreatePool 的 Fee 设置为 30 bps (0.3%)，与需求文档的 25 bps 存在差异，后续版本应修正。

#### 3.2.3 事件数据结构

##### BuyEvent (买入)

```rust
BuyEvent {
    base_amount_out:    u64,       // 8 bytes  - 获得的 base token 数量
    quote_amount_in:    u64,       // 8 bytes  - 花费的 SOL 数量 (lamports)
    lp_fee:             u64,       // 8 bytes  - LP 手续费
    protocol_fee:       u64,       // 8 bytes  - 协议手续费
    pool:               Pubkey,    // 32 bytes - 池子地址
    user:               Pubkey,    // 32 bytes - 交易者
    base_mint:          Pubkey,    // 32 bytes - Base 代币 Mint
    quote_mint:         Pubkey,    // 32 bytes - Quote 代币 Mint (SOL)
}
```

**最小数据长度**: 160 bytes (4 x u64 + 4 x Pubkey = 32 + 128)

**解析流程**:

```go
off := 0
baseAmountOut, off = dex.ParseU64LE(data, off)   // 8 bytes
quoteAmountIn, off = dex.ParseU64LE(data, off)    // 8 bytes
lpFee, off = dex.ParseU64LE(data, off)            // 8 bytes
protocolFee, off = dex.ParseU64LE(data, off)      // 8 bytes
pool, off = dex.ParsePubkey(data, off)            // 32 bytes
user, off = dex.ParsePubkey(data, off)            // 32 bytes
baseMint, off = dex.ParsePubkey(data, off)        // 32 bytes
quoteMint, off = dex.ParsePubkey(data, off)       // 32 bytes
```

**价格计算**:

```
price = (quoteAmountIn / 1e9) / (baseAmountOut / 1e6) = quoteAmountIn / baseAmountOut / 1e3
```

##### SellEvent (卖出)

```rust
SellEvent {
    base_amount_in:     u64,       // 8 bytes  - 卖出的 base token 数量
    quote_amount_out:   u64,       // 8 bytes  - 获得的 SOL 数量 (lamports)
    lp_fee:             u64,       // 8 bytes
    protocol_fee:       u64,       // 8 bytes
    pool:               Pubkey,    // 32 bytes
    user:               Pubkey,    // 32 bytes
    base_mint:          Pubkey,    // 32 bytes
    quote_mint:         Pubkey,    // 32 bytes
}
```

**最小数据长度**: 160 bytes

**价格计算**:

```
price = (quoteAmountOut / 1e9) / (baseAmountIn / 1e6) = quoteAmountOut / baseAmountIn / 1e3
```

##### CreatePoolEvent (创建池子)

```rust
CreatePoolEvent {
    creator:              Pubkey,  // 32 bytes - 创建者
    base_mint:            Pubkey,  // 32 bytes
    quote_mint:           Pubkey,  // 32 bytes
    lp_token_amount_out:  u64,     // 8 bytes  - 初始 LP Token 数量
    pool:                 Pubkey,  // 32 bytes - 池子地址
    lp_mint:              Pubkey,  // 32 bytes - LP Token Mint
    base_amount_in:       u64,     // 8 bytes  - 初始 Base 数量
    quote_amount_in:      u64,     // 8 bytes  - 初始 Quote 数量
}
```

**最小数据长度**: 184 bytes (5 x Pubkey + 3 x u64 = 160 + 24)

##### DepositEvent (添加流动性)

```rust
DepositEvent {
    base_amount_in:       u64,     // 8 bytes
    quote_amount_in:      u64,     // 8 bytes
    lp_token_amount_out:  u64,     // 8 bytes
    pool:                 Pubkey,  // 32 bytes
    user:                 Pubkey,  // 32 bytes
}
```

**最小数据长度**: 88 bytes (3 x u64 + 2 x Pubkey = 24 + 64)

**价值估算**: 使用 `quoteValue * 2` (假设 AMM 中两侧等值) 作为近似总价值。

##### WithdrawEvent (移除流动性)

```rust
WithdrawEvent {
    lp_token_amount_in:   u64,     // 8 bytes
    base_amount_out:      u64,     // 8 bytes
    quote_amount_out:     u64,     // 8 bytes
    pool:                 Pubkey,  // 32 bytes
    user:                 Pubkey,  // 32 bytes
}
```

**最小数据长度**: 88 bytes

#### 3.2.4 数据字段映射

##### BuyEvent/SellEvent -> model.Transaction

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| base_mint | `Addr` | Base 代币 Mint 地址 |
| Program ID | `Router`, `Factory` | PumpSwap Program ID |
| pool | `Pool` | 池子地址 |
| user | `From` | 交易者 |
| Buy: `"buy"` / Sell: `"sell"` | `Side` | 方向 |
| quote_amount (as big.Int) | `Amount` | SOL 数量 (lamports) |
| 计算结果 | `Price` | SOL/代币 价格 |
| LamportsToSOL(quoteAmount) | `Value` | SOL 价值 |
| quote_mint | `Extra.QuoteAddr` | Quote 代币地址 |
| 6 | `Extra.TokenDecimals` | 代币精度 |

##### CreatePoolEvent -> model.Pool

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| pool | `Addr` | 池子地址 |
| Program ID | `Factory` | PumpSwap Program ID |
| `"pumpswap"` | `Protocol` | 协议名 |
| `{0: base_mint, 1: quote_mint}` | `Tokens` | 代币对 |
| 30 | `Fee` | 手续费 (bps) |
| creator | `Extra.From` | 创建者 |

##### DepositEvent/WithdrawEvent -> model.Liquidity

| 事件字段 | Model 字段 | 说明 |
|---------|-----------|------|
| pool | `Addr`, `Pool` | 池子地址 |
| Program ID | `Router`, `Factory` | PumpSwap Program ID |
| user | `From` | 用户 |
| Deposit: `"add"` / Withdraw: `"remove"` | `Side` | 方向 |
| quoteAmount (big.Int) | `Amount` | SOL 数量 |
| `quoteValue * 2` | `Value` | 估算总价值 |
| `"{hash}_add_{eventIdx}"` 或 `"{hash}_remove_{eventIdx}"` | `Key` | 唯一键 |

---

## 4. Sui 链 DEX 协议

Sui 链使用 Move 语言编写智能合约，事件机制与 EVM 和 Solana 都不同：

| 特性 | Sui |
|------|-----|
| 事件标识 | Move 事件类型字符串 (如 `{package}::events::AssetSwap`) |
| 数据格式 | JSON (parsedJson) |
| 地址格式 | 0x 前缀的 64 字符十六进制 (32 bytes) |
| 代币标识 | 完整类型路径 (如 `0x2::sui::SUI`) |
| 事件序号 | eventSeq (字符串形式的整数) |

### 4.1 Bluefin

**实现文件**: `internal/parser/dexs/suidex/bluefin.go`
**Extractor 类**: `BluefinExtractor` (独立实现，未使用 BaseDexExtractor 继承体系)
**AMM 合约地址**: `0x3492c874c1e3b3e2984e8c41b589e642d4d0a5d6459e5a9cfc2d52fd7c89c267`
**协议标识**: `["bluefin"]`
**类型**: CLMM (集中流动性做市商)

#### 4.1.1 事件类型

| 事件 | Move 类型字符串 | 说明 |
|------|----------------|------|
| PoolCreated | `{ammAddr}::events::PoolCreated` | 池子创建 |
| AssetSwap | `{ammAddr}::events::AssetSwap` | 普通 swap |
| FlashSwap | `{ammAddr}::events::FlashSwap` | 闪电 swap |
| LiquidityProvided | `{ammAddr}::events::LiquidityProvided` | 添加流动性 |
| LiquidityRemoved | `{ammAddr}::events::LiquidityRemoved` | 移除流动性 |

其中 `{ammAddr}` = `0x3492c874c1e3b3e2984e8c41b589e642d4d0a5d6459e5a9cfc2d52fd7c89c267`

#### 4.1.2 事件数据结构 (parsedJson)

Sui 事件数据以 JSON 格式存储于 `parsedJson` 字段中，通过 `map[string]interface{}` 访问。

##### AssetSwap / FlashSwap

| 字段 | 类型 | 说明 |
|------|------|------|
| `pool_id` | string | 池子 Object ID |
| `a2b` | bool | true: coinA->coinB, false: coinB->coinA |
| `amount_in` | string (大整数) | 输入数量 |
| `amount_out` | string (大整数) | 输出数量 |
| `fee` | string (大整数) | 手续费 |
| `pool_coin_a_amount` | string (大整数) | swap 后的 coinA 池子余额 |
| `pool_coin_b_amount` | string (大整数) | swap 后的 coinB 池子余额 |

**Swap 处理特点**:
- 每次 swap 生成**两条** Transaction 记录 (sell 和 buy)
- `a2b=true`: sell token0 (coinA) + buy token1 (coinB)
- `a2b=false`: sell token1 (coinB) + buy token0 (coinA)
- 同时更新 Reserve 记录 (使用事件中的 pool_coin_a_amount/pool_coin_b_amount)

##### LiquidityProvided / LiquidityRemoved

| 字段 | 类型 | 说明 |
|------|------|------|
| `pool_id` | string | 池子 Object ID |
| `coin_a_amount` | string (大整数) | coinA 数量 |
| `coin_b_amount` | string (大整数) | coinB 数量 |

每次流动性事件生成**两条** Liquidity 记录 (分别对应 token0 和 token1)，Key 后缀为 `_0` 和 `_1`。

##### PoolCreated

通过 `pool_id` 从链上获取 Pool Object，解析出：
- 代币类型 (从 Object type 字符串的泛型参数中提取)
- fee_rate (从 Object content.fields 中获取，`fee_rate / 100` 转换为 bps)

#### 4.1.3 特殊机制

##### 链上查询与缓存

Bluefin Extractor 需要通过 Sui RPC 客户端从链上查询池子对象和代币元数据：

```go
type BluefinExtractor struct {
    client      *sui.SuiProcessor         // Sui RPC 客户端
    tokenCache  map[string]*TokenCacheItem // 代币元数据缓存
    poolCache   map[string]*PoolCacheItem  // 池子对象缓存
    cacheMutex  sync.RWMutex
    quoteAssets map[string]int             // addr -> rank
}
```

| 缓存项 | TTL | 用途 |
|--------|-----|------|
| Pool Object | 60 秒 | 避免重复查询同一池子的链上数据 |
| Token Metadata | 1 小时 | 代币 name/symbol/decimals 变化频率低 |

使用前必须调用 `SetSuiProcessor()` 设置 RPC 客户端，否则 `ExtractDexData()` 将返回错误。

##### 代币地址提取

池子的代币地址从 Pool Object 的 type 字符串中提取泛型参数：

```go
func (b *BluefinExtractor) ExtractPoolCoin(coinType string) (string, string) {
    token0, token1 := utils.ExtractPoolTokens(coinType)
    // 特殊处理 SUI 代币简写
    if strings.EqualFold(token0, "0x2::sui::SUI") {
        token0 = "0x2::sui::SUI"
    }
    return token0, token1
}
```

##### 价格与价值计算

```go
// rawToHuman: 将链上原始金额转为人类可读浮点数
// 使用 big.Float 避免大数溢出
func rawToHuman(amount *big.Int, decimals int) float64 {
    exp := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
    result, _ := new(big.Float).Quo(
        new(big.Float).SetInt(amount),
        new(big.Float).SetInt(exp),
    ).Float64()
    return result
}
```

**USD 价值估算规则**:
- `rank >= 90` 的 QuoteAsset 视为 USD 稳定币，金额直接作为 USD 价值
- 两侧都是稳定币时，取 rank 较高的一方金额
- 非稳定币暂时无法估算 USD 价值 (返回 0，需要外部价格源)
- 流动性事件中，如果一侧是稳定币，另一侧可通过比例换算估算价值

#### 4.1.4 数据字段映射

##### AssetSwap/FlashSwap -> model.Transaction (x2)

| 字段 | sell 记录 | buy 记录 |
|------|----------|----------|
| `Addr` | sellAddr (a2b ? token0 : token1) | buyAddr (a2b ? token1 : token0) |
| `Factory` | bluefinAmmAddr | bluefinAmmAddr |
| `Pool` | pool_id | pool_id |
| `Side` | `"sell"` | `"buy"` |
| `Amount` | amount_in | amount_out |
| `Price` | SellPrice (humanOut/humanIn) | BuyPrice (humanIn/humanOut) |
| `Value` | TradeValue (USD 估算) | TradeValue |
| `EventIndex` | parseEventSeq(eventSeq) | parseEventSeq(eventSeq) |
| `From` | sender (从事件中提取) | sender |

##### PoolCreated -> model.Pool

| 字段 | Model 字段 | 说明 |
|------|-----------|------|
| pool_id | `Addr` | 池子 Object ID |
| bluefinAmmAddr | `Factory` | AMM 合约地址 |
| `"bluefin"` | `Protocol` | 协议名 |
| {0: token0, 1: token1} | `Tokens` | 从 Pool Object type 提取 |
| fee_rate / 100 | `Fee` | 手续费率 (bps) |

##### LiquidityProvided/Removed -> model.Liquidity (x2)

对 coinA 和 coinB 分别生成一条记录：

| 字段 | token0 记录 | token1 记录 |
|------|------------|------------|
| `Addr` | token0 地址 | token1 地址 |
| `Pool` | pool_id | pool_id |
| `Amount` | coin_a_amount | coin_b_amount |
| `Value` | USD 价值 (若为稳定币) | USD 价值 (若为稳定币) |
| `Key` | `{hash}_{side}_{seq}_0` | `{hash}_{side}_{seq}_1` |

---

## 5. 跨 DEX 共享组件

### 5.1 EVM 共享工具

**文件**: `internal/parser/dexs/utils.go`

#### 核心函数列表

| 函数 | 签名 | 用途 |
|------|------|------|
| `ExtractEVMLogsFromTransaction` | `(tx *UnifiedTransaction) []*ethtypes.Log` | 从统一交易中提取 EVM 日志，支持多种 RawData 格式 |
| `ParseEVMLogsFromInterface` | `(logs interface{}) []*ethtypes.Log` | 从 JSON 反序列化的 interface{} 解析日志 |
| `ToSignedInt256` | `(b []byte) *big.Int` | 256 位大端字节 -> 有符号 big.Int (二进制补码) |
| `ToSignedInt64` | `(b []byte) int64` | 64 位大端字节 -> 有符号 int64 |
| `CalcPrice` | `(amountIn, amountOut *big.Int) float64` | V2 价格: amountOut / amountIn |
| `CalcV3Price` | `(sqrtPriceX96 *big.Int) float64` | V3 价格: (sqrtPriceX96 / 2^96)^2 |
| `CalcValue` | `(amount *big.Int, price float64) float64` | 价值: amount * price |
| `ConvertDecimals` | `(amount *big.Int, decimals uint8) float64` | 精度转换: amount / 10^decimals |
| `ExtractEventIndex` | `(log *ethtypes.Log) int64` | 提取日志的 Index |
| `GetBlockNumber` | `(tx *UnifiedTransaction) int64` | 安全获取区块号 |
| `ValidateAddress` | `(addr string) bool` | 验证 0x 开头的 42 字符 hex 地址 |
| `SafeStringConversion` | `(v interface{}) string` | 安全类型转换 interface{} -> string |
| `SafeUint256Conversion` | `(v interface{}) *big.Int` | 安全数值转换 interface{} -> *big.Int |
| `ParseHexBytes` | `(hexStr string) []byte` | hex 字符串 -> []byte |
| `ParseHexAddress` | `(hexStr string) common.Address` | hex -> Ethereum Address |
| `BytesToBigInt` | `(b []byte) *big.Int` | 大端字节 -> big.Int |
| `BigIntToBytes` | `(i *big.Int) []byte` | big.Int -> 32 字节大端 (左填充零) |

#### ExtractEVMLogsFromTransaction 支持的 RawData 格式

```go
// 格式 1: map[string]interface{} 含 "logs" 键
// 格式 2: map[string]interface{} 含 "receipt" 键 (值为 *ethtypes.Receipt)
// 格式 3: *ethtypes.Receipt 直接类型
// 格式 4: []*ethtypes.Log 直接类型
```

#### 安全性设计

所有数学计算函数在结果为 NaN 或 Inf 时返回 0：

```go
if math.IsNaN(value) || math.IsInf(value, 0) {
    return 0
}
```

`ConvertDecimals` 对 decimals 上限做了保护 (最大 77，防止指数溢出)。

### 5.2 Solana 共享工具

**文件**: `internal/parser/dexs/solana_extractor.go`

#### 字节解析函数

所有函数均为 offset-based 设计，返回 `(value, newOffset)` 二元组，且在越界时安全返回零值：

| 函数 | 签名 | 说明 |
|------|------|------|
| `ParseU8` | `(data []byte, offset int) (uint8, int)` | 无符号 8 位整数 |
| `ParseBool` | `(data []byte, offset int) (bool, int)` | 布尔值 (1 字节, 非零为 true) |
| `ParseU16LE` | `(data []byte, offset int) (uint16, int)` | 16 位小端序无符号 |
| `ParseU64LE` | `(data []byte, offset int) (uint64, int)` | 64 位小端序无符号 |
| `ParseI64LE` | `(data []byte, offset int) (int64, int)` | 64 位小端序有符号 |
| `ParseU128LE` | `(data []byte, offset int) (Uint128, int)` | 128 位小端序 (返回 Uint128) |
| `ParsePubkey` | `(data []byte, offset int) (string, int)` | 32 字节 -> Base58 字符串 |
| `ParseString` | `(data []byte, offset int) (string, int)` | Borsh 字符串: 4 字节 LE 长度 + UTF-8 |

#### Uint128 类型

```go
type Uint128 struct {
    Low  uint64  // 低 64 位
    High uint64  // 高 64 位
}

func (u Uint128) ToBigInt() *big.Int  // 转为 *big.Int (High << 64 | Low)
func (u Uint128) String() string      // 十进制字符串表示
```

#### 事件提取

```go
// ExtractSolanaEventData 从交易日志中提取事件数据
// 支持的 RawData 格式中的日志字段名:
//   - "log_messages"
//   - "logMessages"
//   - "meta" -> "logMessages"
//   - "meta" -> "log_messages"
func ExtractSolanaEventData(tx *types.UnifiedTransaction) [][]byte
```

处理流程:
1. 从 RawData (map[string]interface{}) 中获取 logMessages 字符串数组
2. 遍历所有日志行，查找 `"Program data: "` 前缀
3. 截取前缀后的 Base64 编码部分并解码
4. 过滤掉长度 < 8 字节的数据 (至少需要 discriminator)
5. 返回所有解码后的事件字节切片

#### Discriminator 匹配

```go
// 包级别函数，无需 receiver
func MatchDiscriminatorBytes(data []byte, expected []byte) bool
```

逐字节比较，`data` 长度不足时返回 false。

#### 单位转换

```go
// LamportsToSOL: 1 SOL = 1,000,000,000 lamports
func LamportsToSOL(lamports uint64) float64 {
    return float64(lamports) / 1e9
}
```

#### 错误类型

```go
type InsufficientDataError struct {
    Needed int    // 需要的字节数
    Got    int    // 实际的字节数
    Field  string // 字段名
}
```

### 5.3 Extractor 继承体系

```
DexExtractors Interface
  |
  +-- BaseDexExtractor (base_extractor.go)
  |     字段: protocols, supportedChains, quoteAssets, log, cacheMutex
  |     方法: SetQuoteAssets, IsChainSupported, GetLogger,
  |           GetQuoteAssetRank, MergeQuoteAssets
  |
  +-- EVMDexExtractor (evm_extractor.go) -- 嵌入 BaseDexExtractor
  |     方法: ExtractEVMLogs, FilterLogsByTopics, IsEVMChainSupported
  |     |
  |     +-- PancakeSwapExtractor (bsc/pancakeswap.go)
  |     +-- FourMemeExtractor (bsc/fourmeme.go)
  |     +-- UniswapExtractor (eth/uniswap.go)
  |
  +-- SolanaDexExtractor (solana_extractor.go) -- 嵌入 BaseDexExtractor
  |     方法: ExtractDiscriminator, MatchDiscriminator, IsSolanaChainSupported
  |     |
  |     +-- PumpFunExtractor (solanadex/pumpfun.go)
  |     +-- PumpSwapExtractor (solanadex/pumpswap.go)
  |
  +-- BluefinExtractor (suidex/bluefin.go) -- 独立实现
        特殊: 需要 Sui RPC 客户端、链上查询、独立缓存
        方法: SetSuiProcessor, SetQuoteAssets, ExtractPoolCoin
```

**设计原则**:
- 使用 Go 的嵌入 (embedding) 实现组合，而非传统继承
- 每个子 Extractor 只需实现 `ExtractDexData()` 和 `SupportsBlock()`
- 共享工具函数 (CalcPrice, ParsePubkey 等) 作为包级函数，无需通过 receiver 调用

---

## 6. 附录

### A. QuoteAssets 配置

QuoteAssets 用于标识各链上的报价资产 (如稳定币、包装原生代币)，在价格计算和价值估算中起关键作用。

#### BSC 链

| 名称 | 地址 | 优先级 (rank) |
|------|------|--------------|
| USDT | `0x55d398326f99059fF775485246999027B3197955` | 100 |
| USDC | `0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d` | 99 |
| BUSD | `0xe9e7CEA3DedcA5984780Bafc599bD69ADd087D56` | 98 |
| WBNB | `0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c` | 95 |

#### Sui 链 (Bluefin 默认)

| 名称 | 地址 | 优先级 (rank) |
|------|------|--------------|
| USDC | `0xdba34672e30cb065b1f93e3ab55318768fd6fef66c15942c9f7cb846e2f900e7::usdc::USDC` | 100 |
| USDT | `0xc060006111016b8a020ad5b33834984a437aaa7d3c74c18e09a95d48aceab08c::coin::COIN` | 99 |
| WUSDC | `0x5d4b302506645c37ff133b98c4b50a5ae14841659738d6d733d59d0d217a93bf::coin::COIN` | 98 |

> **rank >= 90** 的资产在 Bluefin 中被视为 USD 稳定币 (价格约等于 1 USD)。

#### Solana 链

Solana 链的 QuoteAsset 为 SOL (native mint: `So11111111111111111111111111111111`)。
PumpFun 和 PumpSwap 均以 SOL 作为报价资产，金额以 lamports 为单位。

### B. Model 定义参考

```go
// model.Transaction - 交易/Swap 记录
type Transaction struct {
    Addr        string                // 代币/池子地址
    Router      string                // 路由合约地址
    Factory     string                // 工厂合约地址
    Pool        string                // 池子地址
    Hash        string                // 交易哈希
    From        string                // 发起者地址
    Side        string                // "swap" | "buy" | "sell" | "route"
    Amount      *big.Int              // 金额
    Price       float64               // 价格
    Value       float64               // 价值
    Time        uint64                // 时间戳
    Extra       *TransactionExtra     // 附加信息
    EventIndex  int64                 // 事件索引
    TxIndex     int64                 // 交易索引
    SwapIndex   int64                 // Swap 序号
    BlockNumber int64                 // 区块号
}

// model.TransactionExtra - 交易附加信息
type TransactionExtra struct {
    QuoteAddr     string              // 报价资产地址 / 手续费临时存储
    QuotePrice    string              // 报价价格 (高精度字符串)
    Type          string              // 交易类型 ("swap" | "buy" | "sell")
    TokenDecimals int                 // 代币精度
}

// model.Pool - 池子/代币创建记录
type Pool struct {
    Addr     string                   // 池子地址
    Factory  string                   // 工厂地址
    Protocol string                   // 协议名 ("pancakeswap" | "uniswap" | ...)
    Tokens   map[int]string           // 代币地址 {0: token0, 1: token1}
    Args     map[string]interface{}   // 额外参数 (creator, version, uri 等)
    Extra    *PoolExtra               // 附加信息
    Fee      int                      // 手续费率 (bps)
}

// model.PoolExtra - 池子附加信息
type PoolExtra struct {
    Hash string                       // 创建交易哈希
    From string                       // 创建者地址
    Time uint64                       // 创建时间戳
}

// model.Liquidity - 流动性事件记录
type Liquidity struct {
    Addr    string                    // 池子地址
    Router  string                    // 路由合约
    Factory string                    // 工厂合约
    Pool    string                    // 池子地址
    Hash    string                    // 交易哈希
    From    string                    // 发起者
    Pos     string                    // 位置 ID (V3 头寸)
    Side    string                    // "add" | "remove" | "graduate"
    Amount  *big.Int                  // 金额
    Value   float64                   // 价值
    Time    uint64                    // 时间戳
    Key     string                    // 唯一键
    Extra   *LiquidityExtra           // 附加信息
}

// model.LiquidityExtra - 流动性附加信息
type LiquidityExtra struct {
    Key     string                    // 唯一键 (与 Liquidity.Key 相同)
    Amounts *big.Int                  // 附加金额
    Values  []float64                 // 各代币价值
    Time    uint64                    // 时间戳
}

// model.Token - 代币信息
type Token struct {
    Addr      string                  // 代币地址
    Name      string                  // 名称
    Symbol    string                  // 符号
    Decimals  int                     // 精度
    IsStable  bool                    // 是否稳定币
    CreatedAt string                  // 创建时间
    UsdPrice  float64                 // USD 价格
}

// model.Reserve - 储备金
type Reserve struct {
    Addr    string                    // 池子地址
    Amounts map[int]*big.Int          // 各代币储备量 {0: amount0, 1: amount1}
    Time    uint64                    // 时间
    Value   map[int]float64           // 各代币 USD 价值
}
```

### C. Side 字段取值规范

| DEX 类型 | Side 取值 | 含义 |
|----------|----------|------|
| Uniswap/PancakeSwap V2/V3 | `"swap"` | AMM 交换 |
| FourMeme | `"buy"` / `"sell"` | Bonding Curve 买卖 |
| PumpFun | `"buy"` / `"sell"` | Bonding Curve 买卖 |
| PumpSwap | `"buy"` / `"sell"` | AMM 买卖 |
| Bluefin | `"sell"` + `"buy"` | 每次 swap 生成两条记录 |
| Mint 事件 (EVM) | `"add"` | 添加流动性 |
| Burn 事件 (EVM) | `"remove"` | 移除流动性 |
| PumpSwap Deposit | `"add"` | 添加流动性 |
| PumpSwap Withdraw | `"remove"` | 移除流动性 |
| FourMeme LiquidityAdded | `"add"` | 毕业上 DEX |
| PumpFun CompleteEvent | `"graduate"` | Bonding Curve 毕业 |
| Bluefin LiquidityProvided | `"add"` | 添加流动性 |
| Bluefin LiquidityRemoved | `"remove"` | 移除流动性 |
