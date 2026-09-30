// Executes the installed LLBot's actual OneBot handler, node conversion and
// forward-card builder with local substitutes for media upload and QQ sending.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { randomUUID } from 'node:crypto'
import { deflateSync, inflateSync } from 'node:zlib'
import { createContext, runInContext } from 'node:vm'
import { patchLLBotSource } from './patch-llbot-forward.mjs'

function section(source, start, end) {
  const from = source.indexOf(start)
  const to = source.indexOf(end, from + start.length)
  assert.ok(from >= 0 && to > from, `Unrecognized LLBot source: ${start}`)
  return source.slice(from, to)
}

async function exercise(source, targetType, sourceTitle, customPreview) {
  const outgoing = section(source, 'async function transformOutgoingSegments(', '//#endregion')
  const action = section(source, 'var SendForwardMsg = class extends BaseAction {', '//#endregion')
  const forward = section(source, 'function forward(nodes, title, preview, summary, prompt)', '\n\t_SendElement.forward = forward;')
  const build = section(source, 'async [ElementType.MultiForward](data)', '\n\tasync [ElementType.File](data)')
  const elementTypes = { Text: 1, Pic: 2, Video: 5, MultiForward: 16 }
  const schema = new Proxy({}, { get: () => () => ({}) })
  let card
  let uploaded
  const mediaPaths = []
  let sandbox
  const context = {
    logger: { warn: assert.fail },
    store: { createMsgShortId: () => 7 },
    ntMsgApi: {
      uploadForwardMsgs: async (peerUid, isGroup, items) => {
        assert.equal(peerUid, '10001')
        assert.equal(isGroup, targetType === 'group')
        uploaded = items[0].buffer.msg
        return 'fixture-forward-id'
      },
    },
    app: {
      sendMessage: async (ctx, peer, elements) => {
        assert.equal(elements.length, 1)
        const builder = new sandbox.MessageBuilding(ctx, [], peer.chatType, peer.peerUid, new Map())
        await builder[elementTypes.MultiForward](elements[0])
        card = JSON.parse(inflateSync(builder.outputElems[0].lightApp.data.subarray(1)).toString('utf8'))
        return { elements: [{ arkElement: { bytesData: JSON.stringify(card) } }] }
      },
    },
  }
  sandbox = createContext({
    Buffer, crypto: { randomUUID }, deflateSync,
    BaseAction: class { constructor(ctx) { this.ctx = ctx } },
    lib_default$1: schema,
    ActionName: {},
    CreatePeerMode: {},
    ChatType: { Group: 2, C2C: 1 },
    ElementType: elementTypes,
    OB11MessageDataType: { Node: 'node', Image: 'image', Video: 'video' },
    selfInfo: { uin: 99999, nick: '机器人' },
    message2List: (message) => Array.isArray(message) ? message : [message],
    createPeer: async (_ctx, payload) => ({ chatType: payload.message_type === 'group' ? 2 : 1, peerUid: String(payload.group_id ?? payload.user_id) }),
    uri2local: async (_ctx, path) => { mediaPaths.push(path); return { success: true, isLocal: true, path } },
    SendElement: {
      pic: async (_ctx, path) => ({ elementType: elementTypes.Pic, picElement: { sourcePath: path } }),
      video: async (_ctx, path) => ({ elementType: elementTypes.Video, videoElement: { filePath: path } }),
    },
    Msg: { Message: { encode: (message) => message } },
  })
  runInContext(`${forward}\nSendElement.forward = forward;\n${outgoing}\n${action}\n
    var MessageBuilding = class {
      constructor(ctx, elements, chatType, peerUid, nestedForwardTrace) {
        Object.assign(this, { ctx, elements, chatType, peerUid, nestedForwardTrace, outputElems: [] });
      }
      async build() { return { elems: this.elements, content: undefined }; }
      ${build}
    };`, sandbox)
  const Handler = targetType === 'group' ? sandbox.SendGroupForwardMsg : sandbox.SendPrivateForwardMsg
  const result = await new Handler(context)._handle({
    [targetType === 'group' ? 'group_id' : 'user_id']: '10001',
    source: sourceTitle,
    ...(customPreview ? { news: [{ text: '自定义摘要' }], summary: '共 2 条媒体', prompt: '[作者媒体]' } : {}),
    messages: ['image', 'video'].map((kind) => ({
      type: 'node', data: { uin: '20002', name: '分享者名片', content: [{ type: kind, data: { file: `C:/media/original.${kind === 'image' ? 'jpg' : 'mp4'}` } }] },
    })),
  })
  assert.equal(result.forward_id, 'fixture-forward-id')
  assert.equal(result.message_id, 7)
  assert.equal(uploaded.length, 2)
  assert.deepEqual(mediaPaths, ['C:/media/original.jpg', 'C:/media/original.mp4'])
  for (const message of uploaded) {
    assert.equal(message.routingHead.fromUin, 20002)
    assert.equal(message.routingHead.group?.groupCard ?? message.routingHead.c2c?.name, '分享者名片')
  }
  assert.equal(uploaded[0].body.richText.elems[0].elementType, elementTypes.Pic)
  assert.equal(uploaded[1].body.richText.elems[0].elementType, elementTypes.Video)
  return card
}

const [directory] = process.argv.slice(2)
if (!directory) throw new Error('Usage: node scripts/verify-llbot-forward.mjs <LLBot directory>')
const source = readFileSync(join(directory, 'llbot.js'), 'utf8')
const patched = patchLLBotSource(source)
assert.equal(patched.split('\n').length, source.split('\n').length, 'preserve source-map line numbers')
assert.equal(patchLLBotSource(patched), patched, 'patch must be idempotent')
assert.throws(() => patchLLBotSource('unknown distribution'), /does not match/)
assert.throws(() => patchLLBotSource(patched.replace('forward.title = payload.source', 'forward.title = "changed"')), /partial or modified/)
for (const targetType of ['group', 'private']) {
  const defaultTitle = targetType === 'group' ? '群聊的聊天记录' : '聊天记录'
  if (source !== patched) {
    const before = await exercise(source, targetType, '订阅对象昵称', false)
    assert.equal(before.meta.detail.source, defaultTitle, 'reproduce the upstream title loss')
  }
  for (const title of ['订阅对象昵称', '作者 "昵称" & <图文>', '', undefined]) {
    const card = await exercise(patched, targetType, title, false)
    assert.equal(card.meta.detail.source, title ?? defaultTitle)
    assert.equal(card.meta.detail.summary, '查看2条转发消息')
    assert.deepEqual(card.meta.detail.news.map(item => item.text), ['分享者名片: [图片]', '分享者名片: [视频]'])
  }
  const custom = await exercise(patched, targetType, '订阅对象昵称', true)
  assert.deepEqual(custom.meta.detail.news, [{ text: '自定义摘要' }])
  assert.equal(custom.meta.detail.summary, '共 2 条媒体')
  assert.equal(custom.prompt, '[作者媒体]')
}
console.log('PASS: LLBot group/private handlers -> image/video nodes -> encoded forward card; titles, sender IDs/names, previews, defaults and patch drift checks verified. No QQ/network sends performed.')
