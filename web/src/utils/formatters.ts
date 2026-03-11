/**
 * Convert CPU from millicores to a readable format
 * @param cpu CPU value in millicores (e.g., "1500m") or cores (e.g., "2")
 * @returns Formatted CPU string (e.g., "1.5 cores")
 */
export function formatCPU(cpu: string): string {
  if (!cpu) return '0 cores';

  // Handle millicores (e.g., "1500m")
  if (cpu.endsWith('m')) {
    const millicores = parseInt(cpu.slice(0, -1), 10);
    const cores = millicores / 1000;
    return `${cores.toFixed(2)} cores`;
  }

  // Handle cores (e.g., "2")
  const cores = parseFloat(cpu);
  return `${cores.toFixed(2)} cores`;
}

/**
 * Convert memory from Ki/Mi/Gi to GB or MB
 * @param memory Memory value (e.g., "1024Ki", "512Mi", "2Gi")
 * @returns Formatted memory string (e.g., "1.0 MB", "2.0 GB")
 */
export function formatMemory(memory: string): string {
  if (!memory) return '0 MB';

  // Parse the number and unit
  const match = memory.match(/^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti)?$/);
  if (!match) return memory;

  const value = parseFloat(match[1]);
  const unit = match[2] || '';

  let bytes: number;
  switch (unit) {
    case 'Ki':
      bytes = value * 1024;
      break;
    case 'Mi':
      bytes = value * 1024 * 1024;
      break;
    case 'Gi':
      bytes = value * 1024 * 1024 * 1024;
      break;
    case 'Ti':
      bytes = value * 1024 * 1024 * 1024 * 1024;
      break;
    default:
      bytes = value;
  }

  // Convert to GB if >= 1 GB, otherwise MB
  const gb = bytes / (1024 * 1024 * 1024);
  if (gb >= 1) {
    return `${gb.toFixed(2)} GB`;
  }

  const mb = bytes / (1024 * 1024);
  return `${mb.toFixed(2)} MB`;
}

/**
 * Format storage size
 * @param size Storage size (e.g., "10Gi", "500Mi")
 * @returns Formatted size (e.g., "10 GB", "500 MB")
 */
export function formatStorage(size: string): string {
  return formatMemory(size);
}
