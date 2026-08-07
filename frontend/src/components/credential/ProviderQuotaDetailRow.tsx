import { TableCell, TableRow } from '@mui/material';
import type { Provider } from '@/types/provider';
import type { ProviderQuota } from '@/types/quota';
import { QuotaInlineDisplay } from './QuotaInlineDisplay';

interface ProviderQuotaDetailRowProps {
  provider: Provider;
  quota: ProviderQuota | undefined;
  isRefreshing: boolean;
  onRefresh: (providerUuid: string) => void;
  /** Number of columns in the host table. Defaults to 7 (legacy tables). */
  colSpan?: number;
}

/**
 * Detail row component for displaying provider quota information.
 * Renders as a table row with colspan spanning all columns.
 * Only renders if quota data is available.
 */
export function ProviderQuotaDetailRow({
  provider,
  quota,
  isRefreshing,
  onRefresh,
  colSpan = 7,
}: ProviderQuotaDetailRowProps) {
  // Don't render the row if no quota data
  if (!quota) {
    return null;
  }

  return (
    <TableRow>
      <TableCell
        colSpan={colSpan}
        sx={{
          p: 0,
          borderTop: 'none',
        }}
      >
        <QuotaInlineDisplay
          quota={quota}
          isRefreshing={isRefreshing}
          onRefresh={() => onRefresh(provider.uuid)}
        />
      </TableCell>
    </TableRow>
  );
}
