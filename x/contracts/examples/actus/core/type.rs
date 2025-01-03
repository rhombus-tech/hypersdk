use borsh::{BorshDeserialize, BorshSerialize};
use wasmlanche::{Address, Units};

type Timestamp = u64;

// ============= Contract Types =============
#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum ContractType {
    PAM = 0,    // Principal at maturity
    LAM = 1,    // Linear amortizer
    NAM = 2,    // Negative amortizer
    ANN = 3,    // Annuity
    STK = 4,    // Stock
    OPTNS = 5,  // Option
    FUTUR = 6,  // Future
    COM = 7,    // Commodity
    CSH = 8,    // Cash
    CLM = 9,    // Call money
    SWPPV = 10, // Plain Vanilla Swap
    SWAPS = 11, // Swap
    CEG = 12,   // Guarantee
    CEC = 13,   // Collateral
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum ContractRole {
    RPA = 0,  // Real position asset
    RPL = 1,  // Real position liability
    CLO = 2,  // Role of a collateral
    CNO = 3,  // Role of a close-out-netting
    COL = 4,  // Role of an underlying to a collateral
    LG = 5,   // Long position
    ST = 6,   // Short position
    BUY = 7,  // Protection buyer
    SEL = 8,  // Protection seller
    RFL = 9,  // Receive first leg
    PFL = 10, // Pay first leg
    RF = 11,  // Receive fix leg
    PF = 12,  // Pay fix leg
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum DayCountConvention {
    AA = 0,         // Actual/Actual ISDA
    A360 = 1,       // Actual/360
    A365 = 2,       // Actual/365
    E30360ISDA = 3, // 30E/360 ISDA
    E30360 = 4,     // 30E/360
    B252 = 5,       // Business/252
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum EndOfMonthConvention {
    EOM = 0, // End of month
    SD = 1,  // Same day
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum BusinessDayConvention {
    NULL = 0, // No shift
    SCF = 1,  // Shift/calculate following
    SCMF = 2, // Shift/calculate modified following
    CSF = 3,  // Calculate/shift following
    CSMF = 4, // Calculate/shift modified following
    SCP = 5,  // Shift/calculate preceding
    SCMP = 6, // Shift/calculate modified preceding
    CSP = 7,  // Calculate/shift preceding
    CSMP = 8, // Calculate/shift modified preceding
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum Calendar {
    MF = 0, // Monday to Friday
    NC = 1, // No calendar
}

#[derive(Debug, Clone, PartialEq, BorshSerialize, BorshDeserialize)]
pub struct ScheduleConfig {
    pub calendar: Option<Calendar>,
    pub end_of_month_convention: Option<EndOfMonthConvention>,
    pub business_day_convention: Option<BusinessDayConvention>,
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum ContractPerformance {
    PF = 0, // Performant
    DL = 1, // Delayed
    DQ = 2, // Delinquent
    DF = 3, // Default
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum CreditEventTypeCovered {
    DL = 0, // Delayed
    DQ = 1, // Delinquent
    DF = 2, // Default
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum GuaranteedExposure {
    NO = 0, // Nominal value
    NI = 1, // Nominal value plus interest
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum FeeBasis {
    A = 0, // Absolute value
    N = 1, // Notional of underlying
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum InterestCalculationBase {
    NT = 0,    // Calculation base always equals to NT
    NTIED = 1, // Notional remains constant amount as per IED
    NTL = 2,   // Calculation base is notional base lagged
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum ScalingEffect {
    OOO = 0, // No scaling
    IOO = 1, // Only interest payments scaled
    ONO = 2, // Only nominal payments scaled
    OOM = 3, // Only maximum deferred amount scaled
    INO = 4, // Interest and nominal payments scaled
    ONM = 5, // Nominal and maximum deferred amount scaled
    IOM = 6, // Interest and maximum deferred amount scaled
    INM = 7, // Interest, nominal and maximum deferred amount scaled
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum Period {
    D = 0, // Day
    W = 1, // Week
    M = 2, // Month
    Q = 3, // Quarter
    H = 4, // Half year
    Y = 5, // Year
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum PenaltyType {
    A = 0, // Absolute
    N = 1, // Nominal rate
    I = 2, // Current interest rate differential
    O = 3, // No penalty
}

#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum PrepaymentEffect {
    N = 0, // No prepayment
    A = 1, // Prepayment allowed, reduces PRNXT
    M = 2, // Prepayment allowed, reduces MD
}

#[derive(Debug, Clone, PartialEq, BorshSerialize, BorshDeserialize)]
pub struct Cycle {
    pub n: i64,
    pub p: Period,
    pub stub: bool,           // true = short stub, false = long stub
    pub include_end_day: bool,
}

#[derive(Debug, Clone, PartialEq, BorshSerialize, BorshDeserialize)]
pub struct ContractTerms {
    // General
    pub contract_id: Vec<u8>,
    pub contract_type: ContractType,
    pub contract_role: ContractRole,
    pub settlement_currency: Option<Vec<u8>>,

    // Calendar
    pub initial_exchange_date: Option<Timestamp>,
    pub day_count_convention: Option<DayCountConvention>,
    pub schedule_config: ScheduleConfig,

    // Contract Identification
    pub status_date: Timestamp,
    pub market_object_code: Option<Vec<u8>>,

    // Counterparty
    pub contract_performance: Option<ContractPerformance>,
    pub credit_event_type_covered: Option<CreditEventTypeCovered>,
    pub coverage_of_credit_enhancement: Option<Units>,
    pub guaranteed_exposure: Option<GuaranteedExposure>,

    // Fees
    pub cycle_of_fee: Option<Cycle>,
    pub cycle_anchor_date_of_fee: Option<Timestamp>,
    pub fee_accrued: Option<Units>,
    pub fee_basis: Option<FeeBasis>,
    pub fee_rate: Option<Units>,

    // Interest
    pub cycle_anchor_date_of_interest_payment: Option<Timestamp>,
    pub cycle_of_interest_payment: Option<Cycle>,
    pub accrued_interest: Option<Units>,
    pub capitalization_end_date: Option<Timestamp>,
    pub cycle_anchor_date_of_interest_calculation_base: Option<Timestamp>,
    pub cycle_of_interest_calculation_base: Option<Cycle>,
    pub interest_calculation_base: Option<InterestCalculationBase>,
    pub interest_calculation_base_amount: Option<Units>,
    pub nominal_interest_rate: Option<Units>,
    pub nominal_interest_rate2: Option<Units>,
    pub interest_scaling_multiplier: Option<Units>,

    // Dates
    pub maturity_date: Option<Timestamp>,
    pub amortization_date: Option<Timestamp>,
    pub exercise_date: Option<Timestamp>,

    // Notional Principal
    pub notional_principal: Option<Units>,
    pub premium_discount_at_ied: Option<Units>,
    pub cycle_anchor_date_of_principal_redemption: Option<Timestamp>,
    pub cycle_of_principal_redemption: Option<Cycle>,
    pub next_principal_redemption_payment: Option<Units>,
    pub purchase_date: Option<Timestamp>,
    pub price_at_purchase_date: Option<Units>,
    pub termination_date: Option<Timestamp>,
    pub price_at_termination_date: Option<Units>,
    pub quantity: Option<Units>,
    pub currency: Option<Vec<u8>>,
    pub currency2: Option<Vec<u8>>,

    // Scaling
    pub scaling_effect: Option<ScalingEffect>,
    pub scaling_index_at_status_date: Option<Units>,
    pub cycle_anchor_date_of_scaling_index: Option<Timestamp>,
    pub cycle_of_scaling_index: Option<Cycle>,
    pub scaling_index_at_contract_deal_date: Option<Units>,
    pub market_object_code_of_scaling_index: Option<Vec<u8>>,
    pub notional_scaling_multiplier: Option<Units>,

    // Rate Reset
    pub cycle_anchor_date_of_rate_reset: Option<Timestamp>,
    pub cycle_of_rate_reset: Option<Cycle>,
    pub next_reset_rate: Option<Units>,
    pub rate_spread: Option<Units>,
    pub rate_multiplier: Option<Units>,
    pub period_floor: Option<Units>,
    pub period_cap: Option<Units>,
    pub life_cap: Option<Units>,
    pub life_floor: Option<Units>,
    pub market_object_code_of_rate_reset: Option<Vec<u8>>,

    // Penalty
    pub penalty_rate: Option<Units>,
    pub penalty_type: Option<PenaltyType>,
    pub prepayment_effect: Option<PrepaymentEffect>,
}

// Contract state matching Haskell implementation
#[derive(Debug, Clone, PartialEq, BorshSerialize, BorshDeserialize)]
pub struct ContractState {
    pub time_of_maturity: Option<Timestamp>,
    pub notional_principal: Units,
    pub nominal_interest_rate: Units,
    pub accrued_interest: Units,
    pub accrued_interest_first_leg: Option<Units>,
    pub accrued_interest_second_leg: Option<Units>,
    pub last_interest_period: Option<Units>,
    pub fee_accrued: Units,
    pub notional_scaling_multiplier: Units,
    pub interest_scaling_multiplier: Units,
    pub contract_performance: ContractPerformance,
    pub status_date: Timestamp,
    pub next_principal_redemption_payment: Units,
    pub interest_calculation_base: Units,
    pub exercise_date: Option<Timestamp>,
    pub exercise_amount: Option<Units>,
}

// Full Event Type enumeration
#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum EventType {
    IED = 0,   // Initial Exchange
    FP = 1,    // Fee Payment
    PR = 2,    // Principal Redemption
    PD = 3,    // Principal Drawing
    PY = 4,    // Penalty Payment
    PP = 5,    // Principal Prepayment
    IP = 6,    // Interest Payment
    IPFX = 7,  // Interest Payment Fixed Leg
    IPFL = 8,  // Interest Payment Floating Leg
    IPCI = 9,  // Interest Capitalization
    CE = 10,   // Credit Event
    RRF = 11,  // Rate Reset Fixing with Known Rate
    RR = 12,   // Rate Reset Fixing with Unknown Rate
    PRF = 13,  // Principal Payment Amount Fixing
    DV = 14,   // Dividend Payment
    PRD = 15,  // Purchase
    MR = 16,   // Margin Call
    TD = 17,   // Termination
    SC = 18,   // Scaling Index Fixing
    IPCB = 19, // Interest Calculation Base Fixing
    MD = 20,   // Maturity
    XD = 21,   // Exercise
    STD = 22,  // Settlement
    PI = 23,   // Principal Increase
    AD = 24,   // Monitoring
    FX = 25,   // Foreign Exchange Fixing
}
