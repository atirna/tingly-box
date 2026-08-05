import React from 'react';
import ServiceRefControl from './ServiceRefControl';
import type { ServiceRef } from './ServiceRefControl';
import type { Provider } from '@/types/provider';

interface WebProxyControlProps {
    value: ServiceRef | null;
    providers: Provider[];
    disabled?: boolean;
    onChange: (service: ServiceRef | null) => void;
}

const WebProxyControl: React.FC<WebProxyControlProps> = (props) => (
    <ServiceRefControl
        name="Web Proxy"
        offHint="Web Proxy: let a downstream model without web access search and fetch through a web-capable model"
        onHint={(provider, model) => `Web Proxy: search and fetch run on ${provider} / ${model}`}
        pickHint="Choose a model with web access"
        {...props}
    />
);

export default WebProxyControl;
