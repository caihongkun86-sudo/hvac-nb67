// downloadStream.js
//
// 流式下载工具：fetch + ReadableStream 边读边累积 chunks，避免一次性 concat 的 O(n²) 开销。
// 配合后端 /api/.../export 系列端点使用（响应头 X-Total-Rows 表示总行数，供进度展示）。
//
// 重试策略：仅在"连接阶段失败"（fetch 抛错或响应未开始）自动重试 N 次；
// 一旦开始接收响应体数据后中断，直接抛错让用户重新点击，避免半截文件 + 续传复杂度。

/**
 * 流式下载文件
 * @param {string} url 完整请求 URL（含 query string）
 * @param {Object} options
 * @param {number} [options.retries=3] 连接阶段失败重试次数
 * @param {number} [options.retryDelayMs=800] 重试退避起始毫秒
 * @param {(received:number, total:number|null) => void} [options.onProgress] 进度回调
 *   - received: 已接收字节数（粗略）
 *   - total: 服务端 X-Total-Rows 头（CSV 行数；可能为 null）
 * @returns {Promise<{blob: Blob, filename: string, totalRows: number|null, bytes: number}>}
 */
export async function downloadStream(url, options = {}) {
    const {
        retries = 3,
        retryDelayMs = 800,
        onProgress = null,
    } = options

    let attempt = 0
    let response

    // 1. 连接阶段重试（fetch 抛错 / 非 2xx 响应）
    // 一旦 200 OK 并开始读取 body，后续中断不再自动重试
    while (true) {
        try {
            response = await fetch(url, { method: 'GET', credentials: 'same-origin' })
        } catch (err) {
            attempt++
            if (attempt > retries) throw new Error(`连接失败（${retries}次重试后仍失败）: ${err.message}`)
            await sleep(retryDelayMs * attempt)
            continue
        }

        if (!response.ok) {
            // 5xx 视为服务端暂态错误，重试；4xx 视为参数错误，直接失败
            if (response.status >= 500 && attempt < retries) {
                attempt++
                await sleep(retryDelayMs * attempt)
                continue
            }
            const text = await response.text().catch(() => '')
            throw new Error(`服务端返回 ${response.status}: ${text.slice(0, 200)}`)
        }
        break
    }

    // 2. 解析响应头
    const totalRowsHeader = response.headers.get('X-Total-Rows')
    const totalRows = totalRowsHeader ? parseInt(totalRowsHeader, 10) : null
    const disposition = response.headers.get('Content-Disposition') || ''
    const filename = parseFilename(disposition) || `download_${Date.now()}.csv`

    // 3. 流式读取 body
    // 累积 chunks 数组（避免重复 concat），最后一次性 new Blob
    const reader = response.body.getReader()
    const chunks = []
    let received = 0

    while (true) {
        const { done, value } = await reader.read()
        if (done) break
        if (value) {
            chunks.push(value)
            received += value.length
            if (onProgress) {
                try { onProgress(received, totalRows) } catch (_) { /* 进度回调失败不影响下载 */ }
            }
        }
    }

    const blob = new Blob(chunks, { type: 'text/csv;charset=utf-8' })
    return { blob, filename, totalRows, bytes: received }
}

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms))
}

// 从 Content-Disposition 头解析 filename
function parseFilename(disposition) {
    if (!disposition) return ''
    // 优先匹配 filename*=UTF-8''xxx（RFC 5987）
    const star = /filename\*=(?:UTF-8'')?([^;]+)/i.exec(disposition)
    if (star) {
        try { return decodeURIComponent(star[1].trim()) } catch { return star[1].trim() }
    }
    // 兼容 filename="xxx"
    const plain = /filename="?([^";]+)"?/i.exec(disposition)
    if (plain) return plain[1].trim()
    return ''
}

export default downloadStream
