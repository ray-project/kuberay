import { createContext, useContext, useState, ReactNode } from "react";

interface FirstVisitContextType {
    firstVisit: boolean;
    setFirstVisit: (value: boolean) => void;
}

const FirstVisitContext = createContext<FirstVisitContextType | undefined>(undefined);

export const FirstVisitProvider = ({ children }: { children: ReactNode }) => {
    const [firstVisit, setFirstVisit] = useState(true);

    return (
        <FirstVisitContext.Provider value={{ firstVisit, setFirstVisit }}>
            {children}
        </FirstVisitContext.Provider>
    );
}

export const useFirstVisit = () => {
    const context = useContext(FirstVisitContext);
    if (context === undefined) {
        throw new Error("useFirstVisit must be used within a FirstVisitProvider");
    }
    return context;
}
