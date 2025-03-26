use wasmlanche::{state_schema, Address, Context};
use borsh::{BorshSerialize, BorshDeserialize};

// Type aliases for clarity
pub type Timestamp = u64;
pub type Units = u64;

// Enums remain largely unchanged
#[derive(Debug, Clone, Copy, PartialEq, BorshSerialize, BorshDeserialize)]
#[repr(u8)]
pub enum ContractPerformance {
    PF = 0, // Performant
    DL = 1, // Delayed
    DQ = 2, // Delinquent
    DF = 3, // Default
}

// State schema keys mapped to individual state components
state_schema! {
    /// Time of maturity for a contract
    ContractMaturity(Address) => Option<Timestamp>,

    /// Notional principal amount
    NotionalPrincipal(Address) => Units,

    /// Nominal interest rate in basis points
    NominalInterestRate(Address) => Units,

    /// Accrued interest
    AccruedInterest(Address) => Units,

    /// First leg accrued interest for swaps
    AccruedInterestFirstLeg(Address) => Option<Units>,

    /// Second leg accrued interest for swaps
    AccruedInterestSecondLeg(Address) => Option<Units>,

    /// Last interest period
    LastInterestPeriod(Address) => Option<Units>,

    /// Accrued fees
    FeeAccrued(Address) => Units,

    /// Notional scaling multiplier
    NotionalScalingMultiplier(Address) => Units,

    /// Interest scaling multiplier
    InterestScalingMultiplier(Address) => Units,

    /// Contract performance state
    ContractPerformanceState(Address) => ContractPerformance,

    /// Status date
    StatusDate(Address) => Timestamp,

    /// Next principal redemption payment
    NextPrincipalRedemptionPayment(Address) => Units,

    /// Interest calculation base
    InterestCalculationBase(Address) => Units,

    /// Exercise date
    ExerciseDate(Address) => Option<Timestamp>,

    /// Exercise amount
    ExerciseAmount(Address) => Option<Units>,
}

// Helper struct to collect all state for a contract
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

impl ContractState {
    /// Load full contract state from context
    pub fn load(context: &mut Context, address: Address) -> Result<Self, String> {
        Ok(Self {
            time_of_maturity: context
                .get(ContractMaturity(address))
                .map_err(|e| format!("Failed to get maturity: {}", e))?
                .flatten(),
            notional_principal: context
                .get(NotionalPrincipal(address))
                .map_err(|e| format!("Failed to get principal: {}", e))?
                .unwrap_or_default(),
            nominal_interest_rate: context
                .get(NominalInterestRate(address))
                .map_err(|e| format!("Failed to get rate: {}", e))?
                .unwrap_or_default(),
            accrued_interest: context
                .get(AccruedInterest(address))
                .map_err(|e| format!("Failed to get interest: {}", e))?
                .unwrap_or_default(),
            accrued_interest_first_leg: context
                .get(AccruedInterestFirstLeg(address))
                .map_err(|e| format!("Failed to get first leg: {}", e))?
                .flatten(),
            accrued_interest_second_leg: context
                .get(AccruedInterestSecondLeg(address))
                .map_err(|e| format!("Failed to get second leg: {}", e))?
                .flatten(),
            last_interest_period: context
                .get(LastInterestPeriod(address))
                .map_err(|e| format!("Failed to get last period: {}", e))?
                .flatten(),
            fee_accrued: context
                .get(FeeAccrued(address))
                .map_err(|e| format!("Failed to get fee: {}", e))?
                .unwrap_or_default(),
            notional_scaling_multiplier: context
                .get(NotionalScalingMultiplier(address))
                .map_err(|e| format!("Failed to get notional scaling: {}", e))?
                .unwrap_or_default(),
            interest_scaling_multiplier: context
                .get(InterestScalingMultiplier(address))
                .map_err(|e| format!("Failed to get interest scaling: {}", e))?
                .unwrap_or_default(),
            contract_performance: context
                .get(ContractPerformanceState(address))
                .map_err(|e| format!("Failed to get performance: {}", e))?
                .unwrap_or(ContractPerformance::PF),
            status_date: context
                .get(StatusDate(address))
                .map_err(|e| format!("Failed to get status date: {}", e))?
                .unwrap_or_default(),
            next_principal_redemption_payment: context
                .get(NextPrincipalRedemptionPayment(address))
                .map_err(|e| format!("Failed to get next payment: {}", e))?
                .unwrap_or_default(),
            interest_calculation_base: context
                .get(InterestCalculationBase(address))
                .map_err(|e| format!("Failed to get calculation base: {}", e))?
                .unwrap_or_default(),
            exercise_date: context
                .get(ExerciseDate(address))
                .map_err(|e| format!("Failed to get exercise date: {}", e))?
                .flatten(),
            exercise_amount: context
                .get(ExerciseAmount(address))
                .map_err(|e| format!("Failed to get exercise amount: {}", e))?
                .flatten(),
        })
    }

    /// Save full contract state to context
    pub fn save(&self, context: &mut Context, address: Address) -> Result<(), String> {
        context
            .store_by_key(ContractMaturity(address), self.time_of_maturity)
            .map_err(|e| format!("Failed to store maturity: {}", e))?;
        context
            .store_by_key(NotionalPrincipal(address), self.notional_principal)
            .map_err(|e| format!("Failed to store principal: {}", e))?;
        context
            .store_by_key(NominalInterestRate(address), self.nominal_interest_rate)
            .map_err(|e| format!("Failed to store rate: {}", e))?;
        context
            .store_by_key(AccruedInterest(address), self.accrued_interest)
            .map_err(|e| format!("Failed to store interest: {}", e))?;
        context
            .store_by_key(AccruedInterestFirstLeg(address), self.accrued_interest_first_leg)
            .map_err(|e| format!("Failed to store first leg: {}", e))?;
        context
            .store_by_key(AccruedInterestSecondLeg(address), self.accrued_interest_second_leg) 
            .map_err(|e| format!("Failed to store second leg: {}", e))?;
        context
            .store_by_key(LastInterestPeriod(address), self.last_interest_period)
            .map_err(|e| format!("Failed to store last period: {}", e))?;
        context
            .store_by_key(FeeAccrued(address), self.fee_accrued)
            .map_err(|e| format!("Failed to store fee: {}", e))?;
        context
            .store_by_key(NotionalScalingMultiplier(address), self.notional_scaling_multiplier)
            .map_err(|e| format!("Failed to store notional scaling: {}", e))?;
        context
            .store_by_key(InterestScalingMultiplier(address), self.interest_scaling_multiplier)
            .map_err(|e| format!("Failed to store interest scaling: {}", e))?;
        context
            .store_by_key(ContractPerformanceState(address), self.contract_performance)
            .map_err(|e| format!("Failed to store performance: {}", e))?;
        context
            .store_by_key(StatusDate(address), self.status_date)
            .map_err(|e| format!("Failed to store status date: {}", e))?;
        context
            .store_by_key(NextPrincipalRedemptionPayment(address), self.next_principal_redemption_payment)
            .map_err(|e| format!("Failed to store next payment: {}", e))?;
        context
            .store_by_key(InterestCalculationBase(address), self.interest_calculation_base)
            .map_err(|e| format!("Failed to store calculation base: {}", e))?;
        context
            .store_by_key(ExerciseDate(address), self.exercise_date)
            .map_err(|e| format!("Failed to store exercise date: {}", e))?;
        context
            .store_by_key(ExerciseAmount(address), self.exercise_amount)
            .map_err(|e| format!("Failed to store exercise amount: {}", e))?;
        Ok(())
    }
}