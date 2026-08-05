import React from 'react';
import ServiceRefControl from './ServiceRefControl';
import type { ServiceRef } from './ServiceRefControl';
import type { Provider } from '@/types/provider';

/** @deprecated Use ServiceRef — kept so existing imports keep compiling. */
export type VisionService = ServiceRef;

interface VisionProxyControlProps {
    value: ServiceRef | null;
    providers: Provider[];
    disabled?: boolean;
    onChange: (service: ServiceRef | null) => void;
}

const VisionProxyControl: React.FC<VisionProxyControlProps> = (props) => (
    <ServiceRefControl
        name="Vision Proxy"
        offHint="Vision Proxy: describe images via a vision-capable model so text-only downstreams can read them"
        onHint={(provider, model) => `Vision Proxy: images described by ${provider} / ${model}`}
        pickHint="Choose a vision-capable model"
        {...props}
    />
);

export default VisionProxyControl;
