"use client";

// Based off of https://github.com/mui/material-ui/blob/master/docs/data/joy/getting-started/templates/order-dashboard
import { Order } from "@/types/table";
import DeleteIcon from "@mui/icons-material/Delete";
import { Box, Button, Skeleton, Tooltip, Typography } from "@mui/joy";
import Checkbox from "@mui/joy/Checkbox";
import Sheet from "@mui/joy/Sheet";
import Table from "@mui/joy/Table";
import React, { useEffect } from "react";
import FrontendTableHead, { HeadCell } from "./FrontendTableHead";
import { Pagination } from "./Pagination";

interface IFrontendTableProps<T extends { name: string }> {
  data: T[];
  isLoading: boolean;
  error: { message: string; info?: { message: string } } | null;
  headCells: readonly HeadCell<T>[];
  deleteItems: (names: readonly string[]) => Promise<void>;
  deleting: boolean;
  renderRow: (row: T) => React.ReactNode;
  defaultOrderBy: keyof T & string;
  name: string;
}

function descendingComparator<T>(a: T, b: T, orderBy: keyof T) {
  if (b[orderBy] < a[orderBy]) {
    return -1;
  }
  if (b[orderBy] > a[orderBy]) {
    return 1;
  }
  return 0;
}

function getComparator<Key extends keyof any>(
  order: Order,
  orderBy: Key
): (
  a: { [key in Key]: number | string },
  b: { [key in Key]: number | string }
) => number {
  return order === "desc"
    ? (a, b) => descendingComparator(a, b, orderBy)
    : (a, b) => -descendingComparator(a, b, orderBy);
}

// T is the data format for each row, S is the sortable keys
export const FrontendTable = <T extends { name: string }>(
  props: IFrontendTableProps<T>
) => {
  const {
    data,
    isLoading,
    error,
    headCells,
    deleteItems,
    deleting,
    renderRow,
    defaultOrderBy,
    name,
  } = props;
  const [order, setOrder] = React.useState<Order>("desc");
  const [orderBy, setOrderBy] = React.useState<keyof T & string>(
    defaultOrderBy
  );
  const [selected, setSelected] = React.useState<readonly string[]>([]);
  const [page, setPage] = React.useState(1);
  // default to less rows if window is small
  const [rowsPerPage, setRowsPerPage] = React.useState(15);

  // This useEffect is necessary since the window object is not available
  // during static generation. After the component is rendered on browser, we can use it.
  useEffect(() => {
    setRowsPerPage(window?.innerHeight > 800 ? 15 : 10);
  }, []);

  const sortedPaginatedData = React.useMemo(() => {
    const result = data
      .slice() // make a copy
      //@ts-ignore
      .sort(getComparator(order, orderBy))
      .slice((page - 1) * rowsPerPage, (page - 1) * rowsPerPage + rowsPerPage);
    // If you are deleting every job on the page, go back a page
    // Have to put this here or the check could run before
    // the sortedPaginatedData is updated
    if (result.length === 0 && page > 1) {
      setPage(page - 1);
    }
    return result;
  }, [data, order, orderBy, page, rowsPerPage]);

  const handleRequestSort = (
    event: React.MouseEvent<unknown>,
    property: keyof T & string
  ) => {
    const isAsc = orderBy === property && order === "asc";
    setOrder(isAsc ? "desc" : "asc");
    setOrderBy(property);
    setPage(1);
  };

  const numColumns = headCells.length;

  const handleSelectAllClick = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.checked) {
      const newSelected = sortedPaginatedData.map((n) => n.name);
      setSelected(newSelected);
      return;
    }
    setSelected([]);
  };

  const handleClick = (event: React.MouseEvent<unknown>, name: string) => {
    const selectedIndex = selected.indexOf(name);
    let newSelected: readonly string[] = [];

    if (selectedIndex === -1) {
      newSelected = newSelected.concat(selected, name);
    } else if (selectedIndex === 0) {
      newSelected = newSelected.concat(selected.slice(1));
    } else if (selectedIndex === selected.length - 1) {
      newSelected = newSelected.concat(selected.slice(0, -1));
    } else if (selectedIndex > 0) {
      newSelected = newSelected.concat(
        selected.slice(0, selectedIndex),
        selected.slice(selectedIndex + 1)
      );
    }

    setSelected(newSelected);
  };

  const handleChangePage = (newPage: number) => {
    setPage(newPage);
    setSelected([]);
  };

  const handleChangeRowsPerPage = (
    event: React.ChangeEvent<HTMLSelectElement>
  ) => {
    setRowsPerPage(parseInt(event.target.value.toString(), 10));
    setPage(1);
  };

  const isSelected = (name: string) => selected.indexOf(name) !== -1;

  // Avoid a layout jump when reaching the last page with empty rows.
  const emptyRows =
    page > 1 ? Math.max(0, page * rowsPerPage - data.length) : 0;

  const handleDeleteItems = async () => {
    await deleteItems(selected);
    setSelected([]);
  };

  const renderRowsPerPageSelector = () => (
    <>
      <Typography level="body-xs" textColor="neutral.400">
        {labelDisplayedRows({
          from: data.length === 0 ? 0 : (page - 1) * rowsPerPage + 1,
          to: Math.min(data.length, page * rowsPerPage),
          count: data.length,
        })}
      </Typography>
      <Box sx={{ display: "flex", gap: 0.3 }}>
        <Typography level="body-xs" textColor="neutral.400">
          Rows per page:
        </Typography>
        {/* Using native select since I don't want a button look */}
        <select
          value={rowsPerPage}
          onChange={handleChangeRowsPerPage}
          className="bg-transparent outline-none text-neutral-500 text-xs"
        >
          <option value="5">5</option>
          <option value="10">10</option>
          <option value="15">15</option>
          <option value="50">50</option>
        </select>
      </Box>
    </>
  );

  const renderSelectionActions = () => (
    <Button
      size="sm"
      color="danger"
      className="bg-[#C41C1C]"
      startDecorator={<DeleteIcon />}
      sx={{ fontSize: "0.75rem" }}
      loading={deleting}
      onClick={handleDeleteItems}
    >
      Delete {selected.length} {selected.length > 1 ? name : name.slice(0, -1)}
    </Button>
  );

  return (
    <>
      {selected.length > 0 ? (
        <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
          {renderSelectionActions()}
        </Box>
      ) : (
        <Box
          sx={{ display: "flex", justifyContent: "space-between", py: 0.88 }}
        >
          {renderRowsPerPageSelector()}
        </Box>
      )}
      <Sheet
        variant="outlined"
        sx={{
          width: "100%",
          borderRadius: "sm",
          overflow: "auto",
          minHeight: 0,
          my: 1,
        }}
      >
        <Table
          aria-labelledby="tableTitle"
          hoverRow
          stickyHeader
          sx={{
            "--TableCell-headBackground":
              "var(--joy-palette-background-level1)",
            "--Table-headerUnderlineThickness": "1px",
            "--TableRow-hoverBackground":
              "var(--joy-palette-background-level1)",
            "--TableCell-paddingY": "4px",
            "--TableCell-paddingX": "5px",
          }}
        >
          <FrontendTableHead<T>
            numSelected={selected.length}
            //@ts-ignore
            onRequestSort={handleRequestSort}
            onSelectAllClick={handleSelectAllClick}
            order={order}
            orderBy={orderBy}
            rowCount={sortedPaginatedData.length}
            headCells={headCells}
          />
          <tbody>
            {error ? (
              <tr>
                <td colSpan={numColumns + 1} style={{ textAlign: "center" }}>
                  <Typography level="body-sm" color="neutral">
                    {error.message}: {error.info?.message}
                  </Typography>
                </td>
              </tr>
            ) : isLoading ? (
              [...Array(rowsPerPage)].map((e, i) => (
                <tr style={{ height: "40px" }} key={i}>
                  <td colSpan={1} style={{ textAlign: "center" }}>
                    <Skeleton
                      variant="text"
                      sx={{ width: 16, margin: "auto" }}
                    />
                  </td>
                  <td colSpan={numColumns} style={{ textAlign: "center" }}>
                    <Skeleton animation="wave" variant="text" width="95%" />
                  </td>
                </tr>
              ))
            ) : data.length == 0 ? (
              <tr>
                <td colSpan={numColumns + 1} style={{ textAlign: "center" }}>
                  <Typography level="body-sm" color="neutral">
                    There are no {name} to display.
                  </Typography>
                </td>
              </tr>
            ) : (
              <>
                {sortedPaginatedData.map((row, index) => {
                  const isItemSelected = isSelected(row.name);

                  return (
                    <tr key={row.name}>
                      <td style={{ textAlign: "center" }}>
                        <Checkbox
                          size="sm"
                          checked={isItemSelected}
                          slotProps={{
                            checkbox: { sx: { textAlign: "left" } },
                          }}
                          sx={{ verticalAlign: "text-bottom" }}
                          onClick={(event) => handleClick(event, row.name)}
                        />
                      </td>
                      <td className="truncate">
                        <Tooltip variant="outlined" title={row.name}>
                          <span>{row.name}</span>
                        </Tooltip>
                      </td>
                      {renderRow(row)}
                    </tr>
                  );
                })}
                {emptyRows > 0 && (
                  <tr
                    style={
                      {
                        height: `calc(${emptyRows} * 40px)`,
                        "--TableRow-hoverBackground": "transparent",
                      } as React.CSSProperties
                    }
                  >
                    <td colSpan={numColumns} aria-hidden />
                  </tr>
                )}
              </>
            )}
          </tbody>
        </Table>
      </Sheet>
      <Pagination
        totalCount={data.length}
        pageSize={rowsPerPage}
        currentPage={page}
        onPageChange={handleChangePage}
      />
    </>
  );
};

function labelDisplayedRows({
  from,
  to,
  count,
}: {
  from: number;
  to: number;
  count: number;
}) {
  return `${from}–${to} of ${count !== -1 ? count : `more than ${to}`}`;
}
