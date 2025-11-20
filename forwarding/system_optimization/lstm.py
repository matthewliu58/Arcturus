import pandas as pd
import numpy as np
from sklearn.preprocessing import MinMaxScaler
import tensorflow as tf
from tensorflow.keras.models import Sequential
from tensorflow.keras.layers import Dense, Dropout
import matplotlib.pyplot as plt
import re
import matplotlib
from scipy import stats
from sklearn.model_selection import train_test_split
matplotlib.use('Agg')

nginx_data = pd.read_csv('1/test_results_stats_history.csv')
system_data = pd.read_csv('1/system_monitor_20250420_195513.csv')

print("Nginx data shape:", nginx_data.shape)
print("System data shape:", system_data.shape)

nginx_data['Datetime'] = pd.to_datetime(nginx_data['Timestamp'], unit='s')
system_data['Datetime'] = pd.to_datetime(system_data[])

print(nginx_data['Datetime'].min(), "-", nginx_data['Datetime'].max())
print(system_data['Datetime'].min(), "-", system_data['Datetime'].max())

time_diff = (system_data['Datetime'].min() - nginx_data['Datetime'].min()).total_seconds() / 3600
print(f"{time_diff:.2f}")

if abs(round(time_diff) - time_diff) < 0.1 and abs(time_diff) > 1:
    nginx_data['Datetime'] = nginx_data['Datetime'] + pd.Timedelta(hours=round(time_diff))
    print(nginx_data['Datetime'].min(), "-", nginx_data['Datetime'].max())

system_data['CPU_usage'] = system_data[].str.replace('%', '').astype(float)

nginx_simple = nginx_data[['Datetime', 'Requests/s']].rename(columns={'Requests/s': 'Requests_per_second'})
system_simple = system_data[['Datetime', 'CPU_usage']]

nginx_simple = nginx_simple.sort_values('Datetime').copy()
system_simple = system_simple.sort_values('Datetime').copy()

print(nginx_simple.isna().sum())
print(system_simple.isna().sum())

merged_data = pd.merge_asof(
    system_simple,
    nginx_simple,
    on='Datetime',
    direction='nearest',
    tolerance=pd.Timedelta('2min')
)

print(merged_data.shape)
print(merged_data.columns)
print(merged_data.isna().sum())

merged_data = merged_data.dropna()
print(merged_data.shape)

merged_data['delta_req'] = merged_data['Requests_per_second'].diff().fillna(0)

print(merged_data[['CPU_usage', 'delta_req']].describe())


def remove_outliers_iqr(df, column, threshold=1.5):
    q1 = df[column].quantile(0.25)
    q3 = df[column].quantile(0.75)
    iqr = q3 - q1
    lower_bound = q1 - threshold * iqr
    upper_bound = q3 + threshold * iqr

    outliers = df[(df[column] < lower_bound) | (df[column] > upper_bound)]
    if len(outliers) > 0:
        print(len(outliers))

    return df[(df[column] >= lower_bound) & (df[column] <= upper_bound)]


original_len = len(merged_data)
merged_data = remove_outliers_iqr(merged_data, 'delta_req', threshold=2.0)

merged_data = merged_data[(merged_data['CPU_usage'] >= 0) & (merged_data['CPU_usage'] <= 100)]

print(len(merged_data), original_len - len(merged_data))

window_size = 3
merged_data['delta_req_smoothed'] = merged_data['delta_req'].rolling(window=window_size, center=True).mean()

merged_data['delta_req_smoothed'] = merged_data['delta_req_smoothed'].fillna(merged_data['delta_req'])

merged_data['delta_req'] = merged_data['delta_req_smoothed']
merged_data.drop('delta_req_smoothed', axis=1, inplace=True)

log_threshold = 100
large_deltas = merged_data['delta_req'] > log_threshold
if large_deltas.any():
    print(large_deltas.sum())
    merged_data.loc[large_deltas, 'delta_req'] = np.log(merged_data.loc[large_deltas, 'delta_req'])

print(merged_data[['CPU_usage', 'delta_req']].describe())

cpu_filter_mask = (merged_data['CPU_usage'] >= 60) & (merged_data['CPU_usage'] <= 80)
filtered_data = merged_data[cpu_filter_mask]
print(len(filtered_data))
print(filtered_data['CPU_usage'].min(), "-", filtered_data['CPU_usage'].max())

X_data = filtered_data[['CPU_usage', 'delta_req']].values[:-1]
y_data = filtered_data['CPU_usage'].shift(-1).values[:-1].reshape(-1, 1)

print(X_data.shape)
print(y_data.shape)

X_train, X_test, y_train, y_test = train_test_split(X_data, y_data, test_size=0.2, random_state=42)


def analyze_cpu_distribution(data, name, bins=10):
    ranges = np.linspace(60, 80, bins + 1)
    hist, edges = np.histogram(data, bins=ranges)
    for i in range(len(hist)):
        lower = edges[i]
        upper = edges[i + 1]
        count = hist[i]
        percent = count / len(data) * 100
        print(f"{lower:.1f}-{upper:.1f}%: {count} ({percent:.1f}%)")
    return hist


train_dist = analyze_cpu_distribution(X_train[:, 0], "Training set", bins=10)
test_dist = analyze_cpu_distribution(X_test[:, 0], "Test set", bins=10)

print(np.min(y_train), "-", np.max(y_train))
print(np.min(y_test), "-", np.max(y_test))

print(X_train.shape, y_train.shape)
print(X_test.shape, y_test.shape)

scaler_X = MinMaxScaler()
scaler_y = MinMaxScaler()

y_min = np.min(y_train)
y_max = np.max(y_train)

X_train_scaled = scaler_X.fit_transform(X_train)
X_test_scaled = scaler_X.transform(X_test)
y_train_scaled = scaler_y.fit_transform(y_train)
y_test_scaled = scaler_y.transform(y_test)

model = Sequential([
    Dense(64, activation='relu', input_shape=(2,)),
    Dropout(0.2),
    Dense(32, activation='relu'),
    Dropout(0.2),
    Dense(1)
])

model.compile(optimizer='adam', loss='mse')
model.summary()

history = model.fit(
    X_train_scaled, y_train_scaled,
    epochs=50,
    batch_size=32,
    validation_split=0.2,
    verbose=1
)

y_pred_scaled = model.predict(X_test_scaled)


def validate_and_fix_cpu_values(values, min_val=60, max_val=80):
    fixed_values = np.copy(values)

    too_low = fixed_values < min_val
    too_high = fixed_values > max_val

    if np.any(too_low):
        print(np.sum(too_low))
        fixed_values[too_low] = min_val

    if np.any(too_high):
        print(np.sum(too_high))
        fixed_values[too_high] = max_val

    return fixed_values


def safe_inverse_transform(scaler, scaled_values, orig_min, orig_max):
    values = scaler.inverse_transform(scaled_values)

    if np.min(values) < 60 or np.max(values) > 80:
        values = scaled_values * (orig_max - orig_min) + orig_min

    return validate_and_fix_cpu_values(values, min_val=60, max_val=80)


y_pred_actual = safe_inverse_transform(scaler_y, y_pred_scaled, y_min, y_max)
y_test_actual = safe_inverse_transform(scaler_y, y_test_scaled, y_min, y_max)

print(np.min(y_pred_actual), "-", np.max(y_pred_actual))
print(np.min(y_test_actual), "-", np.max(y_test_actual))

plt.figure(figsize=(12, 6))
plt.plot(y_test_actual, label='Actual CPU Usage')
plt.plot(y_pred_actual, label='Predicted CPU Usage')
plt.xlabel('Time Step')
plt.ylabel('CPU Usage (%)')

plt.legend()

plt.savefig('cpu_prediction_60_80_random.png', dpi=300, bbox_inches='tight')
plt.close()

plt.figure(figsize=(12, 6))
plt.plot(history.history['loss'], label='Training Loss')
plt.plot(history.history['val_loss'], label='Validation Loss')
plt.xlabel('Epoch')
plt.ylabel('Loss')
plt.title('Training and Validation Loss (60%-80% CPU Range)')
plt.legend()
plt.grid(True)
plt.savefig('training_history_60_80_random.png', dpi=300, bbox_inches='tight')
plt.close()

from sklearn.metrics import mean_squared_error, mean_absolute_error, r2_score

mse = mean_squared_error(y_test_actual, y_pred_actual)
mae = mean_absolute_error(y_test_actual, y_pred_actual)
rmse = np.sqrt(mse)
r2 = r2_score(y_test_actual, y_pred_actual)

print(f"{mse:.4f}")
print(f"{rmse:.4f}")
print(f"{mae:.4f}")
print(f"{r2:.4f}")

error_by_range = {}
ranges = np.arange(60, 81, 2)
for i in range(len(ranges) - 1):
    lower = ranges[i]
    upper = ranges[i + 1]
    mask = (y_test_actual >= lower) & (y_test_actual < upper)
    if np.sum(mask) > 0:
        range_mse = mean_squared_error(y_test_actual[mask], y_pred_actual[mask])
        range_mae = mean_absolute_error(y_test_actual[mask], y_pred_actual[mask])
        error_by_range[f"{lower}-{upper}%"] = {
            "count": np.sum(mask),
            "mse": range_mse,
            "mae": range_mae
        }

for cpu_range, metrics in error_by_range.items():
    print(f"{cpu_range}: {metrics['count']}, MSE={metrics['mse']:.2f}, MAE={metrics['mae']:.2f}")

for i in range(min(10, len(y_test_actual))):
    print(f"{i + 1}: {y_test_actual[i][0]:.2f}%, {y_pred_actual[i][0]:.2f}%, {y_test_actual[i][0] - y_pred_actual[i][0]:.2f}%")
    