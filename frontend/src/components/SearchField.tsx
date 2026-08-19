import { Search } from '@/components/icons';
import { InputAdornment, TextField, type TextFieldProps } from '@mui/material';

// Unified compact search input: 32px tall with a small search icon.
// The theme owns colors/borders, but "is a search box" is a semantic the
// theme can't express (CSS can't select by purpose), so this component is
// the size contract — every search field in the app should render through
// it so search sizing stays consistent across surfaces.
export function SearchField({ slotProps, sx, ...rest }: TextFieldProps) {
    const inputSlot = (slotProps as Record<string, any> | undefined)?.input ?? {};
    return (
        <TextField
            size="small"
            hiddenLabel
            {...rest}
            slotProps={{
                ...slotProps,
                input: {
                    startAdornment: (
                        <InputAdornment position="start">
                            <Search fontSize="small" />
                        </InputAdornment>
                    ),
                    ...inputSlot,
                    sx: { height: 32, ...inputSlot.sx },
                },
            }}
            sx={sx}
        />
    );
}

export default SearchField;
