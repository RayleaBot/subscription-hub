import { constants, copyFileSync, existsSync, readFileSync, renameSync, unlinkSync, writeFileSync } from 'node:fs'
import { resolve, join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'

const marker = '/* raylea-forward-metadata-v1 */'
const actionAnchor = 'const { sendElements, deleteAfterSentFiles } = await transformOutgoingSegments(this.ctx, nodes, peer, true);'
const brokenTitle = 'source: multiForwardMsgElement.title ?? isGroup ? "群聊的聊天记录" : "聊天记录",'
const fixedTitle = 'source: multiForwardMsgElement.title ?? (isGroup ? "群聊的聊天记录" : "聊天记录"),'
const metadata = [
  marker,
  'for (const element of sendElements) {',
  'const forward = element.multiForwardMsgElement;',
  'if (!forward) continue;',
  'if (payload.source !== undefined) forward.title = payload.source;',
  'if (payload.news !== undefined) forward.preview = payload.news.map(item => item.text);',
  'if (payload.summary !== undefined) forward.summary = payload.summary;',
  'if (payload.prompt !== undefined) forward.prompt = payload.prompt;',
  '}',
].join(' ')

function occurrences(text, part) {
  return text.split(part).length - 1
}

export function patchLLBotSource(source) {
  if (source.includes(marker)) {
    if (occurrences(source, `${actionAnchor} ${metadata}`) !== 1 || occurrences(source, fixedTitle) !== 1 || source.includes(brokenTitle)) {
      throw new Error('LLBot contains a partial or modified forward patch; no files changed')
    }
    return source
  }
  if (occurrences(source, actionAnchor) !== 1 || occurrences(source, brokenTitle) !== 1) {
    throw new Error('LLBot source does not match the verified 8.2.1 implementation; no files changed')
  }
  // Keep line numbers intact for the original distribution source map.
  return source.replace(actionAnchor, `${actionAnchor} ${metadata}`).replace(brokenTitle, fixedTitle)
}

function main(args) {
  const [directory, mode = '--check'] = args
  if (!directory || args.length > 2 || !['--check', '--apply', '--restore'].includes(mode)) {
    throw new Error('Usage: node scripts/patch-llbot-forward.mjs <LLBot directory> [--check|--apply|--restore]')
  }
  const root = resolve(directory)
  const manifest = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'))
  if (manifest.version !== '8.2.1' || manifest.main !== 'llbot.js') {
    throw new Error('Only the verified LLBot 8.2.1 distribution is supported; no files changed')
  }
  const entry = join(root, 'llbot.js')
  const backup = join(root, 'llbot.js.before-forward-metadata.bak')
  const current = readFileSync(entry, 'utf8')
  if (mode === '--restore') {
    const original = readFileSync(backup, 'utf8')
    if (original.includes(marker) || patchLLBotSource(original) !== current) {
      throw new Error('LLBot changed since the patch; refusing to overwrite it')
    }
    copyFileSync(backup, entry)
    console.log('Original LLBot restored; restart LLBot to load it')
    return
  }
  const patched = patchLLBotSource(current)
  if (patched === current) {
    console.log('LLBot forward metadata patch is already present')
    return
  }
  if (mode === '--check') {
    console.log('LLBot 8.2.1 matches both known forward-title defects; patch can be applied')
    return
  }
  const temporary = join(root, 'llbot.forward-metadata-check.mjs')
  writeFileSync(temporary, patched, { encoding: 'utf8', flag: 'wx' })
  try {
    const check = spawnSync(process.execPath, ['--check', temporary], { encoding: 'utf8', windowsHide: true })
    if (check.status !== 0) throw new Error(`Patched LLBot failed syntax validation: ${check.stderr || check.error}`)
    if (readFileSync(entry, 'utf8') !== current) throw new Error('LLBot changed during validation; no files changed')
    if (existsSync(backup)) {
      if (readFileSync(backup, 'utf8') !== current) throw new Error('An unrelated backup already exists; no files changed')
    } else {
      copyFileSync(entry, backup, constants.COPYFILE_EXCL)
    }
    renameSync(temporary, entry)
  } finally {
    if (existsSync(temporary)) unlinkSync(temporary)
  }
  console.log(`Patched LLBot 8.2.1; backup: ${backup}; restart LLBot to load it`)
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { main(process.argv.slice(2)) } catch (error) { console.error(error.message); process.exitCode = 1 }
}
