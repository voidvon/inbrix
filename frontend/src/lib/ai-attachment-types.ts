export const AI_ATTACHMENT_ACCEPT = ".png,.jpg,.jpeg,.webp,.pdf,.doc,.docx,.xls,.xlsx,image/png,image/jpeg,image/webp";

export function isAIImage(contentType: string) {
  return ["image/png", "image/jpeg", "image/webp"].includes(contentType);
}

export function isAllowedAIAttachment(file: File) {
  return isAIImage(file.type) || /\.(png|jpe?g|webp|pdf|docx?|xlsx?)$/i.test(file.name);
}

export function attachmentSize(size: number) {
  return size < 1024 * 1024 ? `${Math.max(1, Math.ceil(size / 1024))} KB` : `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
