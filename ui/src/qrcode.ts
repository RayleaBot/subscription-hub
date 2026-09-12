import qrcode from 'qrcode-generator'

qrcode.stringToBytes = text => Array.from(new TextEncoder().encode(text))

export function encodeQRCode(value: string): string {
  const code = qrcode(0, 'M')
  code.addData(value)
  code.make()
  return code.createDataURL(4, 16)
}
